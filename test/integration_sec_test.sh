#!/usr/bin/env bash
# MiniC Phase 4: Privileged Security Hardening & Capability Dropping Integration Test
# Verifies capability dropping (CapEff, CapPrm, CapInh, CapBnd, CapAmb all zero),
# no_new_privs enforcement (NoNewPrivs: 1), rejection of privileged syscalls (sethostname, mount, raw sockets),
# and complete resource cleanup on a disposable test rootfs.

set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: This integration script requires root privileges to verify container security isolation." >&2
    echo "Please execute: sudo ./test/integration_sec_test.sh" >&2
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if [ ! -d "$PROJECT_ROOT/rootfs" ] || [ ! -f "$PROJECT_ROOT/cmd/minic/main.go" ]; then
    echo "ERROR: Could not establish valid MiniC project root at: $PROJECT_ROOT" >&2
    exit 1
fi

echo "=== MiniC Phase 4 Security Hardening Integration Test ==="

BIN="/tmp/minic-integration-sec"
TEST_BASE=$(mktemp -d /tmp/minic-test-sec.XXXXXX)

cleanup() {
    local exit_code=$?
    rm -f "$BIN"
    if [ -n "${TEST_BASE:-}" ] && [ -d "$TEST_BASE" ]; then
        case "$TEST_BASE" in
            /tmp/minic-test-sec.*)
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

# 2. Prepare disposable test rootfs
echo "[2/6] Preparing disposable test rootfs..."
DISPOSABLE_ROOTFS="$TEST_BASE/disposable_rootfs"
cp -a "$PROJECT_ROOT/rootfs" "$DISPOSABLE_ROOTFS"

if [ "$(realpath "$DISPOSABLE_ROOTFS")" = "$(realpath "$PROJECT_ROOT/rootfs")" ]; then
    echo "FATAL: Disposable rootfs path resolved to project rootfs!" >&2
    exit 1
fi

# Create a standalone Go test fixture with setuid mode to test privilege escalation prevention
FIXTURE_DIR="$TEST_BASE/fixture"
mkdir -p "$FIXTURE_DIR"
cat << 'EOF' > "$FIXTURE_DIR/main.go"
package main

import (
	"fmt"
	"os"
)

func main() {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read status: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(string(data))
}
EOF

SETUID_FIXTURE="$DISPOSABLE_ROOTFS/bin/test_setuid"
go build -buildvcs=false -o "$SETUID_FIXTURE" "$FIXTURE_DIR/main.go"
chmod 4755 "$SETUID_FIXTURE"

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

# 3. Launch container and inspect capability state & no_new_privs
echo "[3/6] Launching container and verifying capability sets & no_new_privs..."
"$BIN" run "$DISPOSABLE_ROOTFS" /bin/sh -c '
    set -euo pipefail

    echo "  [Inside Container] Reading /proc/self/status..."
    STATUS=$(/bin/busybox cat /proc/self/status)
    echo "$STATUS" | /bin/busybox grep -E "Cap|NoNewPrivs"

    # Assertion 1: NoNewPrivs must be 1
    NONUMP=$(echo "$STATUS" | /bin/busybox grep "^NoNewPrivs:" | /bin/busybox awk '\''{print $2}'\'')
    if [ "$NONUMP" != "1" ]; then
        echo "FAIL: Expected NoNewPrivs to be 1, got: \"$NONUMP\"" >&2
        exit 61
    fi
    echo "  -> Assertion 1 PASS: NoNewPrivs is 1."

    # Assertion 2: All capability sets must be all zeros
    for cap_field in CapInh CapPrm CapEff CapBnd CapAmb; do
        VAL=$(echo "$STATUS" | /bin/busybox grep "^${cap_field}:" | /bin/busybox awk '\''{print $2}'\'')
        if [ -z "$VAL" ]; then
            echo "FAIL: Field $cap_field not found in /proc/self/status!" >&2
            exit 62
        fi
        TRIMMED=$(echo "$VAL" | /bin/busybox tr -d "0")
        if [ -n "$TRIMMED" ]; then
            echo "FAIL: $cap_field is not zeroed (value: $VAL)!" >&2
            exit 63
        fi
        echo "  -> Assertion 2 ($cap_field) PASS: $cap_field is 0000000000000000."
    done

    # Assertion 3: Privileged operations must be rejected even though UID is 0
    echo "  [Inside Container] Verifying privileged syscall rejections..."

    # a. Sethostname should be refused (needs CAP_SYS_ADMIN)
    if /bin/busybox hostname hacked 2>/dev/null; then
        echo "FAIL: Container was able to change hostname without CAP_SYS_ADMIN!" >&2
        exit 64
    fi
    echo "  -> Assertion 3a PASS: sethostname refused without CAP_SYS_ADMIN."

    # b. Raw socket should be refused (needs CAP_NET_RAW)
    if /bin/busybox ping -c 1 127.0.0.1 2>/dev/null; then
        echo "FAIL: Container was able to create raw ICMP socket without CAP_NET_RAW!" >&2
        exit 65
    fi
    echo "  -> Assertion 3b PASS: raw ICMP socket refused without CAP_NET_RAW."

    # c. Mount should be refused (needs CAP_SYS_ADMIN)
    if /bin/busybox mount -t tmpfs none /tmp 2>/dev/null; then
        echo "FAIL: Container was able to mount filesystem without CAP_SYS_ADMIN!" >&2
        exit 66
    fi
    echo "  -> Assertion 3c PASS: mount operation refused without CAP_SYS_ADMIN."

    # Assertion 4: Verify setuid execution cannot gain privileges under no_new_privs
    echo "  [Inside Container] Verifying setuid execution cannot gain capabilities..."
    FIXTURE_STATUS=$(/bin/test_setuid)
    FIXTURE_NONUMP=$(echo "$FIXTURE_STATUS" | /bin/busybox grep "^NoNewPrivs:" | /bin/busybox awk '\''{print $2}'\'')
    FIXTURE_EFF=$(echo "$FIXTURE_STATUS" | /bin/busybox grep "^CapEff:" | /bin/busybox awk '\''{print $2}'\'' | /bin/busybox tr -d "0")
    if [ "$FIXTURE_NONUMP" != "1" ] || [ -n "$FIXTURE_EFF" ]; then
        echo "FAIL: Setuid execution gained capabilities under no_new_privs!" >&2
        exit 67
    fi
    echo "  -> Assertion 4 PASS: Setuid executable cannot escalate privileges under no_new_privs."
'

echo "  -> Container security verification succeeded completely."

# 4. Verify host disposable rootfs was NOT mutated
echo "[4/6] Verifying disposable test rootfs integrity..."
generate_manifest "$DISPOSABLE_ROOTFS" > "$DISPOSABLE_MANIFEST_POST"
if ! diff -u "$DISPOSABLE_MANIFEST_PRE" "$DISPOSABLE_MANIFEST_POST"; then
    echo "FAIL: Disposable test rootfs manifest mismatch!" >&2
    exit 1
fi
echo "  -> Disposable rootfs is 100% byte-for-byte identical to baseline."

# 5. Verify original project rootfs remains untouched
echo "[5/6] Verifying original project rootfs remains untouched..."
generate_manifest "$PROJECT_ROOT/rootfs" > "$ORIGINAL_MANIFEST_POST"
if ! diff -u "$ORIGINAL_MANIFEST_PRE" "$ORIGINAL_MANIFEST_POST"; then
    echo "FAIL: Original project rootfs was mutated!" >&2
    exit 1
fi
echo "  -> Original project rootfs is 100% pristine and untouched."

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
echo "=== All Phase 4 Security Hardening Tests Passed Successfully! ==="
