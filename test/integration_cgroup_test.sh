#!/usr/bin/env bash
# MiniC Phase 5: Privileged Cgroups v2 Live Enforcement Integration Test
# Verifies kernel enforcement of Memory (OOM killer / limit constraint),
# CPU quota and throttling (cpu.stat metrics), and PID limit constraints (fork blocking),
# along with complete cleanup on a disposable test rootfs.

set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: This integration script requires root privileges to configure cgroups v2." >&2
    echo "Please execute: sudo ./test/integration_cgroup_test.sh" >&2
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if [ ! -d "$PROJECT_ROOT/rootfs" ] || [ ! -f "$PROJECT_ROOT/cmd/minic/main.go" ]; then
    echo "ERROR: Could not establish valid MiniC project root at: $PROJECT_ROOT" >&2
    exit 1
fi

echo "=== MiniC Cgroups v2 Live Enforcement Integration Test ==="

BIN="/tmp/minic-integration-cgroup"
TEST_BASE=$(mktemp -d /tmp/minic-test-cgroup.XXXXXX)

cleanup() {
    local exit_code=$?
    # Terminate any lingering background child jobs if still running
    if [ -n "${RUNNING_PID:-}" ]; then
        kill "$RUNNING_PID" 2>/dev/null || true
        wait "$RUNNING_PID" 2>/dev/null || true
    fi
    rm -f "$BIN"
    if [ -n "${TEST_BASE:-}" ] && [ -d "$TEST_BASE" ]; then
        case "$TEST_BASE" in
            /tmp/minic-test-cgroup.*)
                rm -rf "$TEST_BASE"
                ;;
        esac
    fi
    exit "$exit_code"
}
trap cleanup EXIT INT TERM

# 1. Build fresh binary without git VCS stamping
echo "[1/7] Building MiniC binary with -buildvcs=false..."
go build -buildvcs=false -o "$BIN" "$PROJECT_ROOT/cmd/minic"

# 2. Prepare disposable test rootfs
echo "[2/7] Preparing disposable test rootfs..."
DISPOSABLE_ROOTFS="$TEST_BASE/disposable_rootfs"
cp -a "$PROJECT_ROOT/rootfs" "$DISPOSABLE_ROOTFS"

if [ "$(realpath "$DISPOSABLE_ROOTFS")" = "$(realpath "$PROJECT_ROOT/rootfs")" ]; then
    echo "FATAL: Disposable rootfs path resolved to project rootfs!" >&2
    exit 1
fi

# Build small Go test fixtures into disposable rootfs
FIXTURES_SRC="$TEST_BASE/fixtures_src"
mkdir -p "$FIXTURES_SRC"

# 2a. Memory allocation fixture: can allocate safe amount (4M) or exceed limit (40M)
cat << 'EOF' > "$FIXTURES_SRC/mem_alloc.go"
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: mem_alloc <safe|exceed>")
		os.Exit(1)
	}

	mode := os.Args[1]
	var size int
	if mode == "safe" {
		size = 4 * 1024 * 1024 // 4 MB
	} else if mode == "exceed" {
		size = 40 * 1024 * 1024 // 40 MB
	} else {
		fmt.Printf("unknown mode: %s\n", mode)
		os.Exit(1)
	}

	buf := make([]byte, size)
	for i := range buf {
		buf[i] = 0xAA
	}

	fmt.Printf("ALLOC_OK: allocated %d bytes\n", len(buf))
}
EOF

# 2b. CPU busy-loop fixture: burns CPU for 500ms to trigger quota throttling
cat << 'EOF' > "$FIXTURES_SRC/cpu_burner.go"
package main

import (
	"fmt"
	"time"
)

func main() {
	start := time.Now()
	target := 500 * time.Millisecond
	for time.Since(start) < target {
		// Tight CPU burn loop
	}
	fmt.Println("CPU_BURN_COMPLETE")
}
EOF

go build -buildvcs=false -o "$DISPOSABLE_ROOTFS/bin/mem_alloc" "$FIXTURES_SRC/mem_alloc.go"
go build -buildvcs=false -o "$DISPOSABLE_ROOTFS/bin/cpu_burner" "$FIXTURES_SRC/cpu_burner.go"

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

# 3. Live PID Limit Verification
echo "[3/7] Verifying live cgroups v2 PID limit enforcement (--pids 4)..."
# In a container with --pids 4, the root shell is PID 1.
# Spawning 3 background processes occupies 4 PIDs total (reaching pids.max=4).
# A subsequent fork attempt MUST be blocked by the kernel (EAGAIN / Resource temporarily unavailable).
set +e
PID_OUT=$("$BIN" run --pids 4 "$DISPOSABLE_ROOTFS" /bin/sh -c '
    /bin/busybox sleep 5 &
    /bin/busybox sleep 5 &
    /bin/busybox sleep 5 &
    /bin/busybox sleep 1
' 2>&1)
set -e

echo "  -> Container output: $PID_OUT"
if echo "$PID_OUT" | grep -q -E "can't fork|Resource temporarily unavailable"; then
    echo "  -> Assertion 1 PASS: PID limit (pids.max=4) actively blocked excess fork via kernel cgroups."
else
    echo "FAIL: Expected fork blockage under pids.max=4, output: $PID_OUT" >&2
    exit 1
fi

# 4. Live Memory Limit Verification
echo "[4/7] Verifying live cgroups v2 Memory limit enforcement (--memory 16M)..."

# Subtest 4a: Allocation below memory limit (4M < 16M) must succeed
SAFE_OUT=$("$BIN" run --memory 16M "$DISPOSABLE_ROOTFS" /bin/mem_alloc safe)
if echo "$SAFE_OUT" | grep -q "ALLOC_OK"; then
    echo "  -> Assertion 2a PASS: Safe allocation (4MB < 16MB) succeeded."
else
    echo "FAIL: Safe allocation failed unexpectedly: $SAFE_OUT" >&2
    exit 1
fi

# Subtest 4b: Allocation exceeding memory limit (40M > 16M) must be killed by kernel OOM killer
set +e
"$BIN" run --memory 16M "$DISPOSABLE_ROOTFS" /bin/mem_alloc exceed >/dev/null 2>&1
EXCEED_EC=$?
set -e

# Linux OOM killer sends SIGKILL (signal 9). Standard exit code is 128 + 9 = 137.
if [ "$EXCEED_EC" -eq 137 ]; then
    echo "  -> Assertion 2b PASS: Allocation exceeding limit (40MB > 16MB) killed by kernel OOM killer (exit code 137)."
else
    echo "FAIL: Expected exit code 137 (SIGKILL by OOM), got: $EXCEED_EC" >&2
    exit 1
fi

# 5. Live CPU Quota and Throttling Verification
echo "[5/7] Verifying live cgroups v2 CPU quota & throttling (--cpu 20)..."

# Launch container with --cpu 20 in background and monitor cgroup stats on host
RUNNING_PID=""
"$BIN" run --cpu 20 "$DISPOSABLE_ROOTFS" /bin/cpu_burner > "$TEST_BASE/cpu_burner.out" 2>&1 &
RUNNING_PID=$!

# Locate active cgroup directory created for this run
CG_PATH=""
for attempt in $(seq 1 30); do
    FOUND=$(find "$CGROUP_ROOT" -maxdepth 1 -name "minic-*" 2>/dev/null | head -n 1 || true)
    if [ -n "$FOUND" ] && [ -f "$FOUND/cpu.max" ] && [ -f "$FOUND/cpu.stat" ]; then
        CG_PATH="$FOUND"
        break
    fi
    sleep 0.05
done

if [ -z "$CG_PATH" ]; then
    echo "FAIL: Could not locate active container cgroup directory under $CGROUP_ROOT!" >&2
    exit 1
fi

# Verify configured CPU quota
CPU_MAX_VAL=$(cat "$CG_PATH/cpu.max")
echo "  -> Active cgroup cpu.max: $CPU_MAX_VAL"
if [ "$CPU_MAX_VAL" != "20000 100000" ]; then
    echo "FAIL: Expected cpu.max '20000 100000' for --cpu 20, got: $CPU_MAX_VAL" >&2
    exit 1
fi
echo "  -> Assertion 3a PASS: cpu.max configured with exact 20% quota (20000/100000)."

# Monitor cpu.stat while cpu_burner runs
THROTTLED=0
for i in $(seq 1 20); do
    if [ -f "$CG_PATH/cpu.stat" ]; then
        STAT_CONTENT=$(cat "$CG_PATH/cpu.stat")
        NR_THROTTLED=$(echo "$STAT_CONTENT" | grep "nr_throttled" | awk '{print $2}' || echo "0")
        THROTTLED_USEC=$(echo "$STAT_CONTENT" | grep "throttled_usec" | awk '{print $2}' || echo "0")
        if [ "$NR_THROTTLED" -gt 0 ] || [ "$THROTTLED_USEC" -gt 0 ]; then
            THROTTLED=1
            echo "  -> Observed live CPU throttling: nr_throttled=$NR_THROTTLED, throttled_usec=$THROTTLED_USEC"
            break
        fi
    fi
    sleep 0.05
done

wait "$RUNNING_PID"
RUNNING_PID=""

if [ "$THROTTLED" -ne 1 ]; then
    echo "FAIL: No CPU throttling recorded in cpu.stat during CPU burn workload!" >&2
    exit 1
fi
echo "  -> Assertion 3b PASS: Kernel actively throttled container CPU execution based on cpu.max quota."

# 6. Verify host disposable rootfs was NOT mutated
echo "[6/7] Verifying disposable test rootfs integrity..."
generate_manifest "$DISPOSABLE_ROOTFS" > "$DISPOSABLE_MANIFEST_POST"
if ! diff -u "$DISPOSABLE_MANIFEST_PRE" "$DISPOSABLE_MANIFEST_POST"; then
    echo "FAIL: Disposable test rootfs manifest mismatch!" >&2
    exit 1
fi
echo "  -> Disposable rootfs is 100% byte-for-byte identical to baseline."

# Verify original project rootfs remains untouched
generate_manifest "$PROJECT_ROOT/rootfs" > "$ORIGINAL_MANIFEST_POST"
if ! diff -u "$ORIGINAL_MANIFEST_PRE" "$ORIGINAL_MANIFEST_POST"; then
    echo "FAIL: Original project rootfs was mutated!" >&2
    exit 1
fi
echo "  -> Original project rootfs is 100% pristine and untouched."

# 7. Verify resource cleanup
echo "[7/7] Verifying cleanup of mounts, staging directories, and cgroups..."
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
echo "=== All Cgroups v2 Live Enforcement Tests Passed Successfully! ==="
