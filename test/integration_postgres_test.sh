#!/usr/bin/env bash
# MiniC Phase 6.3: PostgreSQL Service Integration & Live Network Isolation Test
# Verifies in-container PostgreSQL startup, SQL execution, host-to-container
# network port isolation on 127.0.0.1:15432, clean shutdown, and resource cleanup.

set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: This integration script requires root privileges to manage namespaces and cgroups." >&2
    echo "Please execute: sudo ./test/integration_postgres_test.sh" >&2
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if [ ! -d "$PROJECT_ROOT/rootfs" ] || [ ! -f "$PROJECT_ROOT/cmd/minic/main.go" ]; then
    echo "ERROR: Could not establish valid MiniC project root at: $PROJECT_ROOT" >&2
    exit 1
fi

# Locate PostgreSQL rootfs
PG_ROOTFS=""
is_valid_pg_rootfs() {
    local dir="$1"
    [ -n "$dir" ] && [ -d "$dir" ] && [ -x "$dir/usr/local/bin/minic-postgres-demo" ]
}

if [ -n "${MINIC_POSTGRES_ROOTFS:-}" ] && is_valid_pg_rootfs "$MINIC_POSTGRES_ROOTFS"; then
    PG_ROOTFS="$MINIC_POSTGRES_ROOTFS"
elif [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
    USER_HOME=$(getent passwd "$SUDO_USER" | cut -d: -f6)
    if is_valid_pg_rootfs "$USER_HOME/.cache/minic/rootfs-postgres"; then
        PG_ROOTFS="$USER_HOME/.cache/minic/rootfs-postgres"
    fi
fi

if [ -z "$PG_ROOTFS" ]; then
    for candidate in /home/*/.cache/minic/rootfs-postgres; do
        if is_valid_pg_rootfs "$candidate"; then
            PG_ROOTFS="$candidate"
            break
        fi
    done
fi

if [ -z "$PG_ROOTFS" ] && is_valid_pg_rootfs "$HOME/.cache/minic/rootfs-postgres"; then
    PG_ROOTFS="$HOME/.cache/minic/rootfs-postgres"
fi

if [ -z "$PG_ROOTFS" ]; then
    echo "ERROR: PostgreSQL rootfs not found or demo script missing at ${PG_ROOTFS:-<not found>}" >&2
    echo "Please build the rootfs first: sudo ./scripts/prepare_postgres_rootfs.sh" >&2
    exit 1
fi

echo "=== MiniC Phase 6.3: PostgreSQL Integration & Network Isolation Test ==="
echo "PostgreSQL Rootfs: $PG_ROOTFS"

BIN="/tmp/minic-integration-pg"
TEST_BASE=$(mktemp -d /tmp/minic-test-pg.XXXXXX)
CONTAINER_LOG="$TEST_BASE/container_run.log"
CONTAINER_PID=""

cleanup() {
    local exit_code=$?
    # Ensure background container is stopped if still active
    if [ -n "$CONTAINER_PID" ] && kill -0 "$CONTAINER_PID" 2>/dev/null; then
        kill -TERM "$CONTAINER_PID" 2>/dev/null || true
        wait "$CONTAINER_PID" 2>/dev/null || true
    fi
    rm -f "$BIN"
    if [ -n "${TEST_BASE:-}" ] && [ -d "$TEST_BASE" ]; then
        case "$TEST_BASE" in
            /tmp/minic-test-pg.*)
                rm -rf "$TEST_BASE"
                ;;
        esac
    fi
    exit "$exit_code"
}
trap cleanup EXIT INT TERM

# 1. Build fresh binary without git VCS stamping
echo "[1/7] Building fresh MiniC binary..."
go build -buildvcs=false -o "$BIN" "$PROJECT_ROOT/cmd/minic"

# Baseline manifests and cgroups
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

ORIGINAL_MANIFEST_PRE="$TEST_BASE/original_manifest_pre.txt"
ORIGINAL_MANIFEST_POST="$TEST_BASE/original_manifest_post.txt"
generate_manifest "$PROJECT_ROOT/rootfs" > "$ORIGINAL_MANIFEST_PRE"

CGROUP_ROOT="/sys/fs/cgroup"
PRE_CGROUPS=$(find "$CGROUP_ROOT" -maxdepth 1 -name "minic-*" 2>/dev/null || true)
PRE_STAGING=$(find /tmp -maxdepth 1 -name "minic-run-*" 2>/dev/null || true)

# 2. Verify host port 15432 is unused before test
echo "[2/7] Verifying host TCP port 15432 is not in use..."
is_port_open() {
    local port="$1"
    python3 -c "import socket; s = socket.socket(); s.settimeout(0.5); res = s.connect_ex(('127.0.0.1', $port)); s.close(); exit(0 if res == 0 else 1)" 2>/dev/null
}

if is_port_open 15432; then
    echo "ERROR: Host port 15432 is already occupied by an external service before test started!" >&2
    echo "Cannot verify network isolation. Please terminate the conflicting process." >&2
    exit 1
fi
echo "  -> Assertion PASS: Host port 15432 is free."

# 3. Launch MiniC container in background with hold-seconds for live inspection
echo "[3/7] Launching PostgreSQL container in isolated network namespace..."
"$BIN" run \
    --memory 256M \
    --cpu 100 \
    --pids 64 \
    --uid 70 \
    --gid 70 \
    --hostname minic-postgres \
    "$PG_ROOTFS" \
    /usr/local/bin/minic-postgres-demo --hold-seconds 12 > "$CONTAINER_LOG" 2>&1 &
CONTAINER_PID=$!

echo "  -> MiniC launched (PID $CONTAINER_PID). Waiting for MINIC_POSTGRES_READY marker..."

# 4. Wait reliably for PostgreSQL readiness marker
READY=0
for i in $(seq 1 40); do
    if [ -f "$CONTAINER_LOG" ] && grep -q "MINIC_POSTGRES_READY" "$CONTAINER_LOG"; then
        READY=1
        break
    fi
    if ! kill -0 "$CONTAINER_PID" 2>/dev/null; then
        echo "ERROR: Container process exited prematurely!" >&2
        echo "=== Container Log ==="
        cat "$CONTAINER_LOG" >&2
        exit 1
    fi
    sleep 0.5
done

if [ "$READY" -ne 1 ]; then
    echo "ERROR: Timed out waiting for MINIC_POSTGRES_READY marker!" >&2
    echo "=== Container Log ==="
    cat "$CONTAINER_LOG" >&2
    exit 1
fi
echo "  -> Marker detected: PostgreSQL is running and holding inside container."

# 5. Assert host cannot reach 127.0.0.1:15432 (Network Isolation Assertion)
echo "[4/7] Testing host network isolation against in-container PostgreSQL..."
if is_port_open 15432; then
    echo "FAIL: Host was able to connect to 127.0.0.1:15432! Network namespace isolation failed!" >&2
    echo "=== Container Log ==="
    cat "$CONTAINER_LOG" >&2
    exit 1
fi
echo "  -> Assertion PASS: Host connection to 127.0.0.1:15432 actively refused / unreachable."

# 6. Verify in-container SQL operations from container log
echo "[5/7] Verifying in-container SQL operations and metadata..."

if ! grep -q "127.0.0.1:15432 - accepting connections" "$CONTAINER_LOG"; then
    echo "FAIL: Container did not accept connections on loopback!" >&2
    exit 1
fi
echo "  -> Assertion PASS: In-container server accepted connections on 127.0.0.1:15432."

if ! grep -q "Rahul" "$CONTAINER_LOG" || ! grep -q "Priya" "$CONTAINER_LOG"; then
    echo "FAIL: Student records not found in container output!" >&2
    exit 1
fi
echo "  -> Assertion PASS: Table records (Rahul, Priya) verified in SQL query output."

if ! grep -q "Database Name:       college" "$CONTAINER_LOG"; then
    echo "FAIL: Database 'college' metadata missing!" >&2
    exit 1
fi
echo "  -> Assertion PASS: Database 'college' verified in connection metadata."

# 7. Wait for container clean shutdown
echo "[6/7] Waiting for container hold duration to elapse and clean shutdown..."
wait "$CONTAINER_PID" || {
    EXIT_STATUS=$?
    echo "ERROR: Container exited with non-zero status: $EXIT_STATUS" >&2
    cat "$CONTAINER_LOG" >&2
    exit 1
}
CONTAINER_PID=""

if ! grep -q "PostgreSQL stopped cleanly via pg_ctl" "$CONTAINER_LOG"; then
    echo "FAIL: Clean PostgreSQL shutdown marker missing in container log!" >&2
    cat "$CONTAINER_LOG" >&2
    exit 1
fi
echo "  -> Assertion PASS: Clean pg_ctl database shutdown confirmed."

# 8. Verify host integrity and resource cleanup
echo "[7/7] Verifying host resource cleanup and source rootfs integrity..."

# Host original rootfs integrity
generate_manifest "$PROJECT_ROOT/rootfs" > "$ORIGINAL_MANIFEST_POST"
if ! diff -u "$ORIGINAL_MANIFEST_PRE" "$ORIGINAL_MANIFEST_POST"; then
    echo "FAIL: Original project rootfs was mutated!" >&2
    exit 1
fi
echo "  -> Assertion PASS: Original BusyBox rootfs is 100% pristine and untouched."

# Host OverlayFS mounts cleanup
STALE_MOUNTS=$(grep -E 'minic-run' /proc/mounts || true)
if [ -n "$STALE_MOUNTS" ]; then
    echo "FAIL: Lingering mounts detected in /proc/mounts: $STALE_MOUNTS" >&2
    exit 1
fi
echo "  -> Assertion PASS: No lingering OverlayFS mounts."

# Staging directories cleanup
POST_STAGING=$(find /tmp -maxdepth 1 -name "minic-run-*" 2>/dev/null || true)
NEW_STAGING=$(comm -13 <(echo "$PRE_STAGING" | sort) <(echo "$POST_STAGING" | sort) || true)
if [ -n "$NEW_STAGING" ]; then
    echo "FAIL: Lingering staging directories found: $NEW_STAGING" >&2
    exit 1
fi
echo "  -> Assertion PASS: All temporary staging directories cleaned up."

# Cgroups cleanup
POST_CGROUPS=$(find "$CGROUP_ROOT" -maxdepth 1 -name "minic-*" 2>/dev/null || true)
NEW_CGROUPS=$(comm -13 <(echo "$PRE_CGROUPS" | sort) <(echo "$POST_CGROUPS" | sort) || true)
if [ -n "$NEW_CGROUPS" ]; then
    echo "FAIL: Lingering cgroup directories found: $NEW_CGROUPS" >&2
    exit 1
fi
echo "  -> Assertion PASS: All cgroups v2 control groups removed."

# Verify no leftover postgres processes
LEFTOVER_PG=$(pgrep -f "postgres -p 15432" || true)
if [ -n "$LEFTOVER_PG" ]; then
    echo "FAIL: Lingering PostgreSQL processes found on host: $LEFTOVER_PG" >&2
    exit 1
fi
echo "  -> Assertion PASS: No orphan PostgreSQL server processes."

echo "=== All PostgreSQL Integration & Network Isolation Tests Passed! ==="
