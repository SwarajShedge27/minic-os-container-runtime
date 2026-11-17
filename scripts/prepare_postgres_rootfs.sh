#!/usr/bin/env bash
# MiniC Phase 6.3: PostgreSQL Rootfs Preparation Script
# Prepares a dedicated Alpine-based PostgreSQL root filesystem on native Linux storage
# without container runtime dependencies (Docker, Podman, etc.).
#
# Pinned Alpine Release: v3.24.4 (x86_64 minirootfs)
# Packages: postgresql17=17.12-r0, postgresql17-client=17.12-r0, postgresql17-contrib=17.12-r0
# Target user/group: postgres (UID 70, GID 70)
# Orchestration: /usr/local/bin/minic-postgres-demo

set -euo pipefail

# 1. Require root execution
if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: prepare_postgres_rootfs.sh must be run as root (use sudo)." >&2
    exit 1
fi

# 2. Verify host architecture
ARCH=$(uname -m)
if [ "$ARCH" != "x86_64" ]; then
    echo "ERROR: Unsupported host architecture '$ARCH'. MiniC PostgreSQL rootfs requires x86_64." >&2
    exit 1
fi

# 3. Resolve target directory on native Linux filesystem
if [ -n "${MINIC_POSTGRES_ROOTFS:-}" ]; then
    TARGET_DIR="$MINIC_POSTGRES_ROOTFS"
elif [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
    USER_HOME=$(getent passwd "$SUDO_USER" | cut -d: -f6)
    TARGET_DIR="${USER_HOME}/.cache/minic/rootfs-postgres"
else
    TARGET_DIR="${HOME}/.cache/minic/rootfs-postgres"
fi

PARENT_DIR=$(dirname "$TARGET_DIR")
mkdir -p "$PARENT_DIR"
if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
    SUDO_UID=$(id -u "$SUDO_USER")
    SUDO_GID=$(id -g "$SUDO_USER")
    chown "$SUDO_UID:$SUDO_GID" "$PARENT_DIR" || true
fi

# Validate that target directory is on a genuine native Unix filesystem
FS_TYPE_HEX=$(stat -f -c %t "$PARENT_DIR" 2>/dev/null || true)
FS_TYPE_NAME=$(stat -f -c %T "$PARENT_DIR" 2>/dev/null || true)

if [ "$FS_TYPE_HEX" = "1021997" ] || [ "$FS_TYPE_HEX" = "01021997" ] || [ "$FS_TYPE_NAME" = "9p" ] || [ "$FS_TYPE_NAME" = "drvfs" ]; then
    echo "ERROR: Target directory '$TARGET_DIR' is on a Windows DrvFs/9p mount." >&2
    echo "Windows-mounted filesystems do not support genuine Linux UID/GID ownership metadata." >&2
    echo "MiniC PostgreSQL rootfs must reside on native Linux storage (e.g., ext4 under ~/.cache/minic)." >&2
    exit 1
fi

case "$TARGET_DIR" in
    /mnt/*)
        echo "ERROR: Target directory '$TARGET_DIR' is under /mnt/." >&2
        echo "Windows-mounted filesystems do not support genuine Linux UID/GID ownership metadata." >&2
        echo "Please select a native Linux filesystem path (e.g. in /home or /var)." >&2
        exit 1
        ;;
esac

echo "=== MiniC Phase 6.3: PostgreSQL Rootfs Preparation ==="
echo "Target Architecture: $ARCH"
echo "Target Rootfs Path:  $TARGET_DIR"

# 4. Pinned Alpine Linux minirootfs metadata
ALPINE_BRANCH="v3.24"
ALPINE_VERSION="3.24.4"
ALPINE_ARCH="x86_64"
ALPINE_TARBALL="alpine-minirootfs-${ALPINE_VERSION}-${ALPINE_ARCH}.tar.gz"
ALPINE_URL="https://dl-cdn.alpinelinux.org/alpine/${ALPINE_BRANCH}/releases/${ALPINE_ARCH}/${ALPINE_TARBALL}"
ALPINE_SHA256="cdaa38cb8de3537967ee5b6c9b6cd00cc962da2b018e6eea48696bb274923012"

POSTGRES_PKG_VERSION="17.12-r0"

DOWNLOAD_DIR="$(dirname "$TARGET_DIR")/downloads"
mkdir -p "$DOWNLOAD_DIR"
if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
    chown "$SUDO_UID:$SUDO_GID" "$DOWNLOAD_DIR" || true
fi
TARBALL_FILE="$DOWNLOAD_DIR/$ALPINE_TARBALL"

NEED_DOWNLOAD=1
if [ -f "$TARBALL_FILE" ]; then
    ACTUAL_SHA256=$(sha256sum "$TARBALL_FILE" | cut -d' ' -f1)
    if [ "$ACTUAL_SHA256" = "$ALPINE_SHA256" ]; then
        echo "[1/7] Cached Alpine archive verified ($ALPINE_TARBALL)."
        NEED_DOWNLOAD=0
    else
        echo "[1/7] Cached archive checksum mismatch. Re-downloading..."
        rm -f "$TARBALL_FILE"
    fi
fi

if [ "$NEED_DOWNLOAD" -eq 1 ]; then
    echo "[1/7] Downloading official Alpine minirootfs ${ALPINE_VERSION}..."
    curl -fsSL -o "$TARBALL_FILE" "$ALPINE_URL"
    ACTUAL_SHA256=$(sha256sum "$TARBALL_FILE" | cut -d' ' -f1)
    if [ "$ACTUAL_SHA256" != "$ALPINE_SHA256" ]; then
        echo "ERROR: SHA256 checksum mismatch for $TARBALL_FILE!" >&2
        echo "  Expected: $ALPINE_SHA256" >&2
        echo "  Actual:   $ACTUAL_SHA256" >&2
        rm -f "$TARBALL_FILE"
        exit 1
    fi
    echo "      SHA256 verified: $ALPINE_SHA256"
fi

# 5. Extract minirootfs
echo "[2/7] Extracting minirootfs to $TARGET_DIR..."
rm -rf "$TARGET_DIR"
mkdir -p "$TARGET_DIR"
tar -xzf "$TARBALL_FILE" -C "$TARGET_DIR"

# 6. Configure host DNS and repositories for Alpine v3.24
echo "[3/7] Configuring DNS resolver and APK repositories..."
cp /etc/resolv.conf "$TARGET_DIR/etc/resolv.conf"

cat << EOF > "$TARGET_DIR/etc/apk/repositories"
https://dl-cdn.alpinelinux.org/alpine/${ALPINE_BRANCH}/main
https://dl-cdn.alpinelinux.org/alpine/${ALPINE_BRANCH}/community
EOF

# 7. Install PostgreSQL 17.12 packages via apk
echo "[4/7] Installing pinned PostgreSQL ${POSTGRES_PKG_VERSION} packages via apk in chroot..."
chroot "$TARGET_DIR" /sbin/apk update
chroot "$TARGET_DIR" /sbin/apk add --no-cache \
    "postgresql17=${POSTGRES_PKG_VERSION}" \
    "postgresql17-client=${POSTGRES_PKG_VERSION}" \
    "postgresql17-contrib=${POSTGRES_PKG_VERSION}"

# 8. Configure PostgreSQL user, group, directories, and demo script
echo "[5/7] Configuring user, group, and permissions..."

# Ensure exact user format: postgres:x:70:70:PostgreSQL:/var/lib/postgresql:/bin/sh
if grep -q "^postgres:" "$TARGET_DIR/etc/passwd"; then
    sed -i 's|^postgres:x:70:70:.*$|postgres:x:70:70:PostgreSQL:/var/lib/postgresql:/bin/sh|' "$TARGET_DIR/etc/passwd"
else
    echo "postgres:x:70:70:PostgreSQL:/var/lib/postgresql:/bin/sh" >> "$TARGET_DIR/etc/passwd"
fi

# Ensure exact group format: postgres:x:70:
if grep -q "^postgres:" "$TARGET_DIR/etc/group"; then
    sed -i 's|^postgres:x:70:.*$|postgres:x:70:|' "$TARGET_DIR/etc/group"
else
    echo "postgres:x:70:" >> "$TARGET_DIR/etc/group"
fi

# Configure directories
mkdir -p "$TARGET_DIR/var/lib/postgresql"
chown -R 70:70 "$TARGET_DIR/var/lib/postgresql"
chmod 0700 "$TARGET_DIR/var/lib/postgresql"

mkdir -p "$TARGET_DIR/run/postgresql"
chown -R 70:70 "$TARGET_DIR/run/postgresql"
chmod 0775 "$TARGET_DIR/run/postgresql"

mkdir -p "$TARGET_DIR/tmp"
chmod 1777 "$TARGET_DIR/tmp"
chown 0:0 "$TARGET_DIR/tmp"

# Install in-container demonstration orchestration script
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEMO_SCRIPT="$SCRIPT_DIR/postgres_container_demo.sh"
if [ -f "$DEMO_SCRIPT" ]; then
    echo "  Installing in-container demo script to /usr/local/bin/minic-postgres-demo..."
    mkdir -p "$TARGET_DIR/usr/local/bin"
    cp "$DEMO_SCRIPT" "$TARGET_DIR/usr/local/bin/minic-postgres-demo"
    chmod 0755 "$TARGET_DIR/usr/local/bin/minic-postgres-demo"
    chown 0:0 "$TARGET_DIR/usr/local/bin/minic-postgres-demo"
fi

# Clean up package cache remnants
rm -rf "$TARGET_DIR/var/cache/apk"/* "$TARGET_DIR/etc/apk/cache"/* 2>/dev/null || true

# 9. Record rootfs manifest
echo "[6/7] Generating rootfs release manifest..."
BUILD_TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

cat << EOF > "$TARGET_DIR/etc/minic-rootfs-manifest.json"
{
  "distribution": "Alpine Linux",
  "alpine_version": "${ALPINE_VERSION}",
  "alpine_branch": "${ALPINE_BRANCH}",
  "architecture": "${ALPINE_ARCH}",
  "minirootfs_sha256": "${ALPINE_SHA256}",
  "postgresql_package": "postgresql17",
  "postgresql_version": "${POSTGRES_PKG_VERSION}",
  "build_timestamp": "${BUILD_TIMESTAMP}",
  "user": "postgres",
  "uid": 70,
  "gid": 70,
  "directories": {
    "/var/lib/postgresql": "70:70 (mode 0700)",
    "/run/postgresql": "70:70 (mode 0775)",
    "/tmp": "0:0 (mode 1777)"
  },
  "binaries": {
    "postgres": "/usr/libexec/postgresql17/postgres",
    "initdb": "/usr/libexec/postgresql17/initdb",
    "pg_ctl": "/usr/libexec/postgresql17/pg_ctl",
    "psql": "/usr/libexec/postgresql17/psql",
    "pg_isready": "/usr/libexec/postgresql17/pg_isready",
    "minic-postgres-demo": "/usr/local/bin/minic-postgres-demo"
  }
}
EOF

cat << EOF > "$TARGET_DIR/etc/minic-rootfs-manifest.txt"
MiniC PostgreSQL Rootfs Manifest
--------------------------------
Distribution:       Alpine Linux ${ALPINE_VERSION} (${ALPINE_ARCH})
Alpine Branch:      ${ALPINE_BRANCH}
Minirootfs SHA256:  ${ALPINE_SHA256}
PostgreSQL Package: postgresql17 (${POSTGRES_PKG_VERSION})
User/Group:         postgres:postgres (UID 70, GID 70)
Directories:
  /var/lib/postgresql : 70:70, mode 0700
  /run/postgresql     : 70:70, mode 0775
  /tmp                : 0:0, mode 1777
Binaries:
  postgres            : /usr/libexec/postgresql17/postgres
  initdb              : /usr/libexec/postgresql17/initdb
  pg_ctl              : /usr/libexec/postgresql17/pg_ctl
  psql                : /usr/libexec/postgresql17/psql
  pg_isready          : /usr/libexec/postgresql17/pg_isready
  minic-postgres-demo : /usr/local/bin/minic-postgres-demo
Built:              ${BUILD_TIMESTAMP}
EOF

# 10. Verification
echo "[7/7] Verifying rootfs integrity..."

# User and group checks
UID_VAL=$(chroot "$TARGET_DIR" id -u postgres)
GID_VAL=$(chroot "$TARGET_DIR" id -g postgres)
if [ "$UID_VAL" != "70" ] || [ "$GID_VAL" != "70" ]; then
    echo "ERROR: PostgreSQL account verification failed (UID=$UID_VAL, GID=$GID_VAL)" >&2
    exit 1
fi
echo "  [PASS] User postgres UID=$UID_VAL, GID=$GID_VAL"

# Directory ownership checks
VAR_LIB_OWNER=$(stat -c "%u:%g" "$TARGET_DIR/var/lib/postgresql")
if [ "$VAR_LIB_OWNER" != "70:70" ]; then
    echo "ERROR: /var/lib/postgresql ownership is $VAR_LIB_OWNER, expected 70:70" >&2
    exit 1
fi
echo "  [PASS] /var/lib/postgresql ownership: 70:70 (mode $(stat -c '%a' "$TARGET_DIR/var/lib/postgresql"))"

RUN_PG_OWNER=$(stat -c "%u:%g" "$TARGET_DIR/run/postgresql")
if [ "$RUN_PG_OWNER" != "70:70" ]; then
    echo "ERROR: /run/postgresql ownership is $RUN_PG_OWNER, expected 70:70" >&2
    exit 1
fi
echo "  [PASS] /run/postgresql ownership: 70:70 (mode $(stat -c '%a' "$TARGET_DIR/run/postgresql"))"

TMP_MODE=$(stat -c "%a" "$TARGET_DIR/tmp")
if [ "$TMP_MODE" != "1777" ]; then
    echo "ERROR: /tmp mode is $TMP_MODE, expected 1777" >&2
    exit 1
fi
echo "  [PASS] /tmp permissions: 1777"

# Binaries checks inside chroot
REQUIRED_COMMANDS=("postgres" "initdb" "pg_ctl" "psql" "pg_isready")
for cmd in "${REQUIRED_COMMANDS[@]}"; do
    if ! chroot "$TARGET_DIR" test -x "/usr/bin/$cmd"; then
        echo "ERROR: Required command '$cmd' is not executable at /usr/bin/$cmd inside rootfs" >&2
        exit 1
    fi
    REAL_TARGET=$(chroot "$TARGET_DIR" readlink -f "/usr/bin/$cmd")
    VERSION_INFO=$(chroot "$TARGET_DIR" "/usr/bin/$cmd" --version)
    echo "  [PASS] $cmd (/usr/bin/$cmd -> $REAL_TARGET): $VERSION_INFO"
done

# Verify /usr/local/bin/minic-postgres-demo
if ! chroot "$TARGET_DIR" test -x "/usr/local/bin/minic-postgres-demo"; then
    echo "ERROR: /usr/local/bin/minic-postgres-demo is missing or not executable" >&2
    exit 1
fi
echo "  [PASS] In-container demo script verified at /usr/local/bin/minic-postgres-demo"

# Verify PostgreSQL 17.12 versions specifically
SERVER_VERSION=$(chroot "$TARGET_DIR" /usr/bin/postgres --version)
CLIENT_VERSION=$(chroot "$TARGET_DIR" /usr/bin/psql --version)

if ! echo "$SERVER_VERSION" | grep -q "17.12"; then
    echo "ERROR: Unexpected PostgreSQL server version: $SERVER_VERSION (expected 17.12)" >&2
    exit 1
fi
if ! echo "$CLIENT_VERSION" | grep -q "17.12"; then
    echo "ERROR: Unexpected PostgreSQL client version: $CLIENT_VERSION (expected 17.12)" >&2
    exit 1
fi
echo "  [PASS] Server version confirmed: $SERVER_VERSION"
echo "  [PASS] Client version confirmed: $CLIENT_VERSION"

# Dynamic linker and shared libraries check
echo "  Checking dynamic library dependencies for /usr/bin/postgres..."
MISSING_LIBS=$(chroot "$TARGET_DIR" ldd /usr/bin/postgres 2>&1 | grep -i "not found" || true)
if [ -n "$MISSING_LIBS" ]; then
    echo "ERROR: Missing libraries detected for /usr/bin/postgres:" >&2
    echo "$MISSING_LIBS" >&2
    exit 1
fi
echo "  [PASS] Dynamic linker and all required shared libraries verified."

echo "=========================================================="
echo "PostgreSQL 17.12 (Alpine 3.24.4) rootfs successfully prepared at:"
echo "  $TARGET_DIR"
echo "Manifest: $TARGET_DIR/etc/minic-rootfs-manifest.txt"
echo "Demo:     $TARGET_DIR/usr/local/bin/minic-postgres-demo"
echo "=========================================================="
