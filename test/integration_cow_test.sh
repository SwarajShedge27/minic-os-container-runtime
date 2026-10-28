#!/usr/bin/env bash
# MiniC Phase 2.1: Privileged Integration Verification Script
# Verifies OverlayFS Copy-On-Write isolation, minimal device support,
# ephemeral staging cleanup, and complete source rootfs protection.

set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: This integration script requires root privileges to configure namespaces, OverlayFS mounts, and cgroups v2." >&2
    echo "Please execute: sudo ./test/integration_cow_test.sh" >&2
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if [ ! -d "$PROJECT_ROOT/rootfs" ] || [ ! -f "$PROJECT_ROOT/cmd/minic/main.go" ]; then
    echo "ERROR: Could not establish valid MiniC project root at: $PROJECT_ROOT" >&2
    exit 1
fi

echo "=== MiniC Phase 2 Integration Test ==="

BIN="/tmp/minic-integration"
TEST_BASE=$(mktemp -d /tmp/minic-test-cow.XXXXXX)

cleanup() {
    local exit_code=$?
    rm -f "$BIN"
    if [ -n "${TEST_BASE:-}" ] && [ -d "$TEST_BASE" ]; then
        case "$TEST_BASE" in
            /tmp/minic-test-cow.*)
                rm -rf "$TEST_BASE"
                ;;
        esac
    fi
    exit "$exit_code"
}
trap cleanup EXIT INT TERM

# 1. Build fresh binary without git VCS stamping to avoid dubious ownership under root
echo "[1/6] Building MiniC binary with -buildvcs=false..."
go build -buildvcs=false -o "$BIN" "$PROJECT_ROOT/cmd/minic"

# 2. Prepare disposable test rootfs
echo "[2/6] Preparing disposable test rootfs..."
DISPOSABLE_ROOTFS="$TEST_BASE/disposable_rootfs"
cp -a "$PROJECT_ROOT/rootfs" "$DISPOSABLE_ROOTFS"

# Explicit safety guard: ensure DISPOSABLE_ROOTFS is never the original project rootfs
if [ "$(realpath "$DISPOSABLE_ROOTFS")" = "$(realpath "$PROJECT_ROOT/rootfs")" ]; then
    echo "FATAL: Disposable rootfs path resolved to project rootfs!" >&2
    exit 1
fi

# Add dedicated test marker files to the disposable rootfs
MARKER_MODIFY="$DISPOSABLE_ROOTFS/marker_modify.txt"
MARKER_DELETE="$DISPOSABLE_ROOTFS/marker_delete.txt"
echo "baseline_marker_data_initial" > "$MARKER_MODIFY"
echo "baseline_marker_data_to_delete" > "$MARKER_DELETE"

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

MANIFEST_PRE="$TEST_BASE/manifest_pre.txt"
MANIFEST_POST="$TEST_BASE/manifest_post.txt"
generate_manifest "$DISPOSABLE_ROOTFS" > "$MANIFEST_PRE"
echo "  -> Baseline manifest recorded ($(wc -l < "$MANIFEST_PRE") entries)."

ORIGINAL_MANIFEST_PRE="$TEST_BASE/original_manifest_pre.txt"
ORIGINAL_MANIFEST_POST="$TEST_BASE/original_manifest_post.txt"
generate_manifest "$PROJECT_ROOT/rootfs" > "$ORIGINAL_MANIFEST_PRE"

CGROUP_ROOT="/sys/fs/cgroup"
PRE_CGROUPS=$(find "$CGROUP_ROOT" -maxdepth 1 -name "minic-*" 2>/dev/null || true)
PRE_STAGING=$(find /tmp -maxdepth 1 -name "minic-run-*" 2>/dev/null || true)

# 3. Launch container with disposable rootfs
echo "[3/6] Running MiniC container with devices and in-container file operations..."
"$BIN" run "$DISPOSABLE_ROOTFS" /bin/sh -c '
    set -euo pipefail

    # Device operations
    echo "device-null-test" > /dev/null
    /bin/busybox head -c 16 /dev/zero > /dev/null
    /bin/busybox head -c 16 /dev/urandom > /dev/null

    # File creation
    echo "created_in_container_data" > /created_in_container.txt

    # File modification
    echo "appended_mutated_data" >> /marker_modify.txt

    # File deletion
    /bin/busybox rm /marker_delete.txt

    # In-container assertions
    if [ ! -f /created_in_container.txt ]; then
        echo "ASSERTION FAILED: /created_in_container.txt missing inside container" >&2
        exit 41
    fi
    if [ -f /marker_delete.txt ]; then
        echo "ASSERTION FAILED: /marker_delete.txt still exists inside container" >&2
        exit 42
    fi
    if ! /bin/busybox grep -q "appended_mutated_data" /marker_modify.txt; then
        echo "ASSERTION FAILED: /marker_modify.txt not modified inside container" >&2
        exit 43
    fi
    echo "  -> In-container operations and assertions verified successfully."
'

# 4. Verify host disposable rootfs was NOT mutated
echo "[4/6] Verifying copy-on-write isolation on disposable rootfs..."

if [ -e "$DISPOSABLE_ROOTFS/created_in_container.txt" ]; then
    echo "FAIL: /created_in_container.txt leaked into host test rootfs!" >&2
    exit 1
fi

if [ ! -f "$MARKER_DELETE" ]; then
    echo "FAIL: $MARKER_DELETE was deleted from host test rootfs!" >&2
    exit 1
fi

if grep -q "appended_mutated_data" "$MARKER_MODIFY"; then
    echo "FAIL: $MARKER_MODIFY was modified in host test rootfs!" >&2
    exit 1
fi

generate_manifest "$DISPOSABLE_ROOTFS" > "$MANIFEST_POST"
if ! diff -u "$MANIFEST_PRE" "$MANIFEST_POST"; then
    echo "FAIL: Disposable test rootfs manifest mismatch after container exit!" >&2
    exit 1
fi
echo "  -> Disposable test rootfs is 100% byte-for-byte identical to pre-run baseline."

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
echo "=== All Phase 2 Integration Tests Passed Successfully! ==="
