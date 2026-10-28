#!/usr/bin/env bash
# MiniC Phase 3: Privileged Network Isolation Integration Verification Script
# Verifies Linux Network Namespace isolation (CLONE_NEWNET), loopback interface configuration,
# loopback connectivity (127.0.0.1), absence of host network interfaces, and host network integrity.

set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: This integration script requires root privileges to configure network namespaces." >&2
    echo "Please execute: sudo ./test/integration_net_test.sh" >&2
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if [ ! -d "$PROJECT_ROOT/rootfs" ] || [ ! -f "$PROJECT_ROOT/cmd/minic/main.go" ]; then
    echo "ERROR: Could not establish valid MiniC project root at: $PROJECT_ROOT" >&2
    exit 1
fi

echo "=== MiniC Phase 3 Network Isolation Integration Test ==="

BIN="/tmp/minic-integration-net"
TEST_BASE=$(mktemp -d /tmp/minic-test-net.XXXXXX)

cleanup() {
    local exit_code=$?
    rm -f "$BIN"
    if [ -n "${TEST_BASE:-}" ] && [ -d "$TEST_BASE" ]; then
        case "$TEST_BASE" in
            /tmp/minic-test-net.*)
                rm -rf "$TEST_BASE"
                ;;
        esac
    fi
    exit "$exit_code"
}
trap cleanup EXIT INT TERM

# 1. Build fresh binary without git VCS stamping
echo "[1/6] Building MiniC binary with -buildvcs=false..."
go build -buildvcs=false -o "$BIN" "$PROJECT_ROOT/cmd/minic"

# 2. Record host network baseline
echo "[2/6] Recording host network baseline..."
HOST_NETNS=$(readlink /proc/self/ns/net)
echo "  -> Host network namespace: $HOST_NETNS"

# Identify host non-loopback interfaces (e.g. eth0) to verify they are hidden in container
HOST_NON_LO_IFACES=$(grep ':' /proc/net/dev | awk -F: '{print $1}' | tr -d ' ' | grep -v '^lo$' || true)
if [ -n "$HOST_NON_LO_IFACES" ]; then
    echo "  -> Host non-loopback interfaces detected: $(echo "$HOST_NON_LO_IFACES" | tr '\n' ' ')"
fi

# 3. Prepare disposable test rootfs
echo "[3/6] Preparing disposable test rootfs..."
DISPOSABLE_ROOTFS="$TEST_BASE/disposable_rootfs"
cp -a "$PROJECT_ROOT/rootfs" "$DISPOSABLE_ROOTFS"

if [ "$(realpath "$DISPOSABLE_ROOTFS")" = "$(realpath "$PROJECT_ROOT/rootfs")" ]; then
    echo "FATAL: Disposable rootfs path resolved to project rootfs!" >&2
    exit 1
fi

generate_manifest() {
    local target_dir="$1"
    (
        cd "$target_dir"
        find . -mindepth 1 | sort | while read -r item; do
            if [ -L "$item" ]; then
                echo "LINK $item -> $(readlink "$item") [mode $(stat -c '%a' "$item")]"
            elif [ -d "$item" ]; then
                echo "DIR  $item [mode $(stat -c '%a' "$item")]"
            elif [ -f "$item" ]; then
                echo "FILE $item [mode $(stat -c '%a' "$item")] sha256:$(sha256sum "$item" | cut -d' ' -f1)"
            else
                echo "SPECIAL $item [type $(stat -c '%F' "$item")]"
            fi
        done
    )
}

DISPOSABLE_MANIFEST_PRE="$TEST_BASE/manifest_pre.txt"
DISPOSABLE_MANIFEST_POST="$TEST_BASE/manifest_post.txt"
generate_manifest "$DISPOSABLE_ROOTFS" > "$DISPOSABLE_MANIFEST_PRE"

ORIGINAL_MANIFEST_PRE="$TEST_BASE/original_manifest_pre.txt"
ORIGINAL_MANIFEST_POST="$TEST_BASE/original_manifest_post.txt"
generate_manifest "$PROJECT_ROOT/rootfs" > "$ORIGINAL_MANIFEST_PRE"

CGROUP_ROOT="/sys/fs/cgroup"
PRE_CGROUPS=$(find "$CGROUP_ROOT" -maxdepth 1 -name "minic-*" 2>/dev/null || true)
PRE_STAGING=$(find /tmp -maxdepth 1 -name "minic-run-*" 2>/dev/null || true)

# 4. Launch MiniC container and test network namespace isolation
echo "[4/6] Executing container with network isolation assertions..."
"$BIN" run "$DISPOSABLE_ROOTFS" /bin/sh -c "
    set -euo pipefail

    echo '  [Inside Container] Checking network namespace...'
    CONTAINER_NETNS=\$(/bin/busybox readlink /proc/self/ns/net)
    echo \"  [Inside Container] Container netns: \$CONTAINER_NETNS\"

    # Assertion 1: Container netns must differ from host netns
    if [ \"\$CONTAINER_NETNS\" = \"$HOST_NETNS\" ]; then
        echo \"FAIL: Container shares network namespace with host! (\$CONTAINER_NETNS)\" >&2
        exit 51
    fi
    echo '  -> Assertion 1 PASS: Distinct network namespace verified.'

    # Assertion 2: Verify visible interfaces in /proc/net/dev
    echo '  [Inside Container] Inspecting network interfaces...'
    VISIBLE_IFACES=\$(/bin/busybox awk -F: 'NR>2 {gsub(/ /, \"\", \$1); print \$1}' /proc/net/dev)
    echo \"  [Inside Container] Visible interfaces: \$VISIBLE_IFACES\"

    # Loopback must be present
    if ! echo \"\$VISIBLE_IFACES\" | /bin/busybox grep -q '^lo\$'; then
        echo 'FAIL: Loopback interface (lo) is missing in container!' >&2
        exit 52
    fi

    # Host non-loopback interfaces must NOT be present
    for host_if in $HOST_NON_LO_IFACES; do
        if echo \"\$VISIBLE_IFACES\" | /bin/busybox grep -q \"^\$host_if\$\"; then
            echo \"FAIL: Host interface \$host_if is leaked into container network namespace!\" >&2
            exit 53
        fi
    done
    echo '  -> Assertion 2 PASS: Host interfaces hidden, only isolated interfaces visible.'

    # Assertion 3: Verify loopback interface is UP and configured with 127.0.0.1
    IFCONFIG_LO=\$(/bin/busybox ifconfig lo)
    echo \"\$IFCONFIG_LO\"
    if ! echo \"\$IFCONFIG_LO\" | /bin/busybox grep -q 'UP'; then
        echo 'FAIL: Loopback interface is not UP in container!' >&2
        exit 54
    fi
    if ! echo \"\$IFCONFIG_LO\" | /bin/busybox grep -q '127.0.0.1'; then
        echo 'FAIL: Loopback interface has no IPv4 loopback address (127.0.0.1)!' >&2
        exit 55
    fi
    echo '  -> Assertion 3 PASS: Loopback interface is UP with 127.0.0.1.'

    # Assertion 4: Verify unprivileged loopback communication (TCP roundtrip without CAP_NET_RAW)
    echo '  [Inside Container] Testing unprivileged loopback TCP communication on 127.0.0.1:18080...'
    /bin/busybox nc -l -p 18080 -e /bin/busybox echo 'minic-loopback-ok' &
    NC_PID=\$!
    /bin/busybox sleep 0.2
    RECV=\$(/bin/busybox nc -w 2 127.0.0.1 18080 || true)
    kill \$NC_PID 2>/dev/null || true
    echo \"  [Inside Container] Received from loopback: \$RECV\"
    if ! echo \"\$RECV\" | /bin/busybox grep -q 'minic-loopback-ok'; then
        echo 'FAIL: Unprivileged loopback TCP connection to 127.0.0.1:18080 failed!' >&2
        exit 56
    fi
    echo '  -> Assertion 4 PASS: Unprivileged loopback TCP communication verified.'
"

echo "  -> Container network verification succeeded completely."

# 5. Verify host network configuration was unaltered
echo "[5/6] Verifying host network configuration was unaltered..."
HOST_NETNS_POST=$(readlink /proc/self/ns/net)
if [ "$HOST_NETNS" != "$HOST_NETNS_POST" ]; then
    echo "FAIL: Host network namespace changed from $HOST_NETNS to $HOST_NETNS_POST!" >&2
    exit 1
fi

HOST_IFACES_POST=$(grep ':' /proc/net/dev | awk -F: '{print $1}' | tr -d ' ' | grep -v '^lo$' || true)
if [ "$HOST_NON_LO_IFACES" != "$HOST_IFACES_POST" ]; then
    echo "FAIL: Host interfaces were altered by container run!" >&2
    exit 1
fi
echo "  -> Host network configuration remains completely untouched."

# Verify disposable rootfs was not mutated
generate_manifest "$DISPOSABLE_ROOTFS" > "$DISPOSABLE_MANIFEST_POST"
if ! diff -u "$DISPOSABLE_MANIFEST_PRE" "$DISPOSABLE_MANIFEST_POST"; then
    echo "FAIL: Disposable test rootfs manifest mismatch!" >&2
    exit 1
fi

# Verify original project rootfs was not mutated
generate_manifest "$PROJECT_ROOT/rootfs" > "$ORIGINAL_MANIFEST_POST"
if ! diff -u "$ORIGINAL_MANIFEST_PRE" "$ORIGINAL_MANIFEST_POST"; then
    echo "FAIL: Original project rootfs was mutated!" >&2
    exit 1
fi
echo "  -> Project rootfs integrity verified 100% pristine."

# 6. Verify resource cleanup
echo "[6/6] Verifying cleanup of mounts, staging directories, and cgroups..."
STALE_MOUNTS=$(grep -E 'minic-run|disposable_rootfs' /proc/mounts || true)
if [ -n "$STALE_MOUNTS" ]; then
    echo "FAIL: Lingering mounts detected in /proc/mounts: $STALE_MOUNTS" >&2
    exit 1
fi

POST_STAGING=$(find /tmp -maxdepth 1 -name "minic-run-*" 2>/dev/null || true)
NEW_STAGING=$(comm -13 <(echo "$PRE_STAGING" | sort) <(echo "$POST_STAGING" | sort) || true)
if [ -n "$NEW_STAGING" ]; then
    echo "FAIL: Lingering staging directories found: $NEW_STAGING" >&2
    exit 1
fi

POST_CGROUPS=$(find "$CGROUP_ROOT" -maxdepth 1 -name "minic-*" 2>/dev/null || true)
NEW_CGROUPS=$(comm -13 <(echo "$PRE_CGROUPS" | sort) <(echo "$POST_CGROUPS" | sort) || true)
if [ -n "$NEW_CGROUPS" ]; then
    echo "FAIL: Lingering cgroup directories found: $NEW_CGROUPS" >&2
    exit 1
fi

echo "  -> Mounts, temporary staging directories, and cgroups cleaned up cleanly."
echo "=== All Phase 3 Network Isolation Tests Passed Successfully! ==="
