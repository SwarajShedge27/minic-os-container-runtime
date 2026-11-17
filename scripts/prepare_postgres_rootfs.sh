#!/usr/bin/env bash
# MiniC Phase 6.2: PostgreSQL Rootfs Preparation Script
# Prepares a dedicated Alpine-based PostgreSQL root filesystem on native Linux storage
# without container runtime dependencies (Docker, Podman, etc.).
#
# Pinned Alpine Release: v3.20.10 (x86_64 minirootfs)
# Packages: postgresql16, postgresql16-client, postgresql16-contrib
# Target user/group: postgres (UID 70, GID 70)

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

echo "=== MiniC Phase 6.2: PostgreSQL Rootfs Preparation ==="
echo "Target Architecture: $ARCH"
echo "Target Rootfs Path:  $TARGET_DIR"

# 4. Pinned Alpine Linux minirootfs metadata
ALPINE_BRANCH="v3.20"
ALPINE_VERSION="3.20.10"
ALPINE_ARCH="x86_64"
ALPINE_TARBALL="alpine-minirootfs-${ALPINE_VERSION}-${ALPINE_ARCH}.tar.gz"
ALPINE_URL="https://dl-cdn.alpinelinux.org/alpine/${ALPINE_BRANCH}/releases/${ALPINE_ARCH}/${ALPINE_TARBALL}"
ALPINE_SHA256="0d44a414245f043ae56872a23762e6c9bee585da2cbe4aa81cbd0331eaf53223"

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
        echo "[1/6] Cached Alpine archive verified ($ALPINE_TARBALL)."
        NEED_DOWNLOAD=0
    else
        echo "[1/6] Cached archive checksum mismatch. Re-downloading..."
        rm -f "$TARBALL_FILE"
    fi
fi

if [ "$NEED_DOWNLOAD" -eq 1 ]; then
    echo "[1/6] Downloading official Alpine minirootfs ${ALPINE_VERSION}..."
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
echo "[2/6] Extracting minirootfs to $TARGET_DIR..."
rm -rf "$TARGET_DIR"
mkdir -p "$TARGET_DIR"
tar -xzf "$TARBALL_FILE" -C "$TARGET_DIR"

# 6. Configure host DNS for chroot package installation
echo "[3/6] Configuring DNS resolver..."
cp /etc/resolv.conf "$TARGET_DIR/etc/resolv.conf"

# 7. Install PostgreSQL packages via apk
echo "[4/6] Installing PostgreSQL packages via apk in chroot..."
chroot "$TARGET_DIR" /sbin/apk update
chroot "$TARGET_DIR" /sbin/apk add --no-cache postgresql16 postgresql16-client postgresql16-contrib

# 8. Configure PostgreSQL user, group and directories
echo "[5/6] Configuring user, group, and permissions..."

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

# Clean up package cache remnants
rm -rf "$TARGET_DIR/var/cache/apk"/* "$TARGET_DIR/etc/apk/cache"/* 2>/dev/null || true

# 9. Verification
echo "[6/6] Verifying rootfs integrity..."

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
echo "PostgreSQL rootfs successfully prepared at:"
echo "  $TARGET_DIR"
echo "Phase 6.2 complete. Database startup and SQL execution reserved for Phase 6.3."
echo "=========================================================="
