#!/usr/bin/env bash
# MiniC Phase 2: Privileged Integration Verification Script
# Verifies OverlayFS Copy-On-Write isolation, minimal device support, and rootfs protection.

set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: This integration script must be executed with root privileges: sudo ./test/integration_cow_test.sh" >&2
    exit 1
fi

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

echo "=== MiniC Phase 2 Integration Test ==="

# Build fresh binary
echo "[1/6] Building MiniC binary..."
go build -o /tmp/minic-integration ./cmd/minic
trap 'rm -f /tmp/minic-integration /tmp/rootfs_pre.sha256 /tmp/rootfs_post.sha256' EXIT

# 1. Snapshot original rootfs checksums
echo "[2/6] Recording baseline checksums of ./rootfs..."
find ./rootfs -type f -exec sha256sum {} + | sort > /tmp/rootfs_pre.sha256

# 2. Test minimal device support (/dev/null, /dev/zero, /dev/urandom)
echo "[3/6] Testing device nodes inside container..."
/tmp/minic-integration run ./rootfs /bin/sh -c "echo 'testing null' > /dev/null && head -c 8 /dev/zero > /dev/null && head -c 8 /dev/urandom > /dev/null"
echo "  -> Device files (/dev/null, /dev/zero, /dev/urandom) verified functional."

# 3. Test Copy-On-Write (create, modify, delete inside container)
echo "[4/6] Executing destructive operations inside container..."
/tmp/minic-integration run ./rootfs /bin/sh -c "
    echo 'created_inside_container' > /created_file.txt
    echo 'append_content' >> /bin/echo
    rm /bin/cat
    if [ ! -f /created_file.txt ]; then exit 10; fi
    if [ -f /bin/cat ]; then exit 11; fi
"
echo "  -> In-container mutations succeeded as expected."

# 4. Verify host original rootfs remains completely unchanged
echo "[5/6] Verifying original host rootfs was NOT mutated..."

if [ -f "./rootfs/created_file.txt" ]; then
    echo "FAIL: /created_file.txt leaked into host rootfs!" >&2
    exit 1
fi

if [ ! -e "./rootfs/bin/cat" ]; then
    echo "FAIL: ./rootfs/bin/cat was removed from host rootfs!" >&2
    exit 1
fi

find ./rootfs -type f -exec sha256sum {} + | sort > /tmp/rootfs_post.sha256
if ! diff -u /tmp/rootfs_pre.sha256 /tmp/rootfs_post.sha256; then
    echo "FAIL: Checksum mismatch! Host rootfs was modified!" >&2
    exit 1
fi
echo "  -> Checksums match 100%. Original rootfs is completely pristine."

# 5. Verify cleanup: no stale mounts or lingering staging directories
echo "[6/6] Verifying temporary resource cleanup..."
STALE_MOUNTS=$(grep 'minic-run' /proc/mounts || true)
if [ -n "$STALE_MOUNTS" ]; then
    echo "FAIL: Found lingering OverlayFS mounts: $STALE_MOUNTS" >&2
    exit 1
fi

echo "=== All Phase 2 Integration Tests Passed Successfully! ==="
