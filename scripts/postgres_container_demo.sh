#!/bin/sh
# MiniC Phase 6.3: In-Container PostgreSQL Lifecycle & SQL Demonstration Script
# Designed for POSIX /bin/sh inside Alpine Linux under UID 70 / GID 70 (postgres).
#
# NOTE: Trust authentication is configured strictly for this ephemeral,
# network-isolated container demonstration and is not suitable for production.

set -eu

HOLD_SECONDS=0
PGDATA="/tmp/minic-pgdata"
PGLOG="/tmp/postgres.log"
PGPORT=15432
PGHOST="127.0.0.1"
PGUSER="postgres"

# Parse command line arguments
while [ $# -gt 0 ]; do
    case "$1" in
        --hold-seconds)
            if [ $# -lt 2 ]; then
                echo "ERROR: --hold-seconds requires a numeric argument" >&2
                exit 1
            fi
            HOLD_SECONDS="$2"
            shift 2
            ;;
        --hold-seconds=*)
            HOLD_SECONDS="${1#*=}"
            shift 1
            ;;
        *)
            echo "ERROR: Unknown option: $1" >&2
            echo "Usage: $0 [--hold-seconds N]" >&2
            exit 1
            ;;
    esac
done

# Validate hold duration
case "$HOLD_SECONDS" in
    ''|*[!0-9]*)
        echo "ERROR: Invalid hold duration '$HOLD_SECONDS'. Must be a non-negative integer." >&2
        exit 1
        ;;
esac

if [ "$HOLD_SECONDS" -gt 3600 ]; then
    echo "ERROR: Hold duration $HOLD_SECONDS exceeds maximum allowed (3600s)." >&2
    exit 1
fi

echo "============================================================"
echo "      MiniC PostgreSQL 17 Container Demonstration"
echo "============================================================"
echo "Container Hostname: $(hostname)"
echo "Process User:       $(id)"
echo "Hold Duration:      ${HOLD_SECONDS}s"
echo "Data Directory:     $PGDATA (ephemeral container storage - OverlayFS writable layer)"
echo "============================================================"

# Cleanup handler on EXIT, INT, TERM
STOPPED=0
cleanup() {
    exit_code=$?
    trap - EXIT INT TERM
    if [ "$STOPPED" -eq 0 ]; then
        STOPPED=1
        echo ""
        echo "[MiniC Demo] Initiating graceful database shutdown..."
        if [ -f "$PGDATA/postmaster.pid" ]; then
            if pg_ctl -D "$PGDATA" -m fast -w stop; then
                echo "[MiniC Demo] [PASS] PostgreSQL stopped cleanly via pg_ctl."
            else
                echo "[MiniC Demo] [WARN] Fast stop failed; forcing immediate shutdown..." >&2
                pg_ctl -D "$PGDATA" -m immediate stop || true
            fi
        else
            echo "[MiniC Demo] Postmaster process not running; no stop needed."
        fi
        rm -rf "$PGDATA" "$PGLOG" 2>/dev/null || true
        echo "[MiniC Demo] [PASS] Ephemeral database storage cleaned up."
    fi
    exit "$exit_code"
}
trap cleanup EXIT INT TERM

# Step 1: Initialize ephemeral database cluster
echo "[1/5] Initializing database cluster with initdb..."
rm -rf "$PGDATA"
mkdir -p "$PGDATA"
chmod 0700 "$PGDATA"

# Note: Trust authentication is used exclusively for this isolated demonstration
initdb -D "$PGDATA" \
    --encoding=UTF8 \
    --no-locale \
    --username="$PGUSER" \
    --auth=trust >/dev/null

echo "      [PASS] Cluster initialized at $PGDATA (UTF-8, superuser: $PGUSER)."

# Step 2: Start PostgreSQL server on isolated loopback
echo "[2/5] Starting PostgreSQL server on $PGHOST:$PGPORT..."
pg_ctl -D "$PGDATA" \
    -l "$PGLOG" \
    -o "-p $PGPORT -h $PGHOST -c dynamic_shared_memory_type=mmap -c shared_buffers=16MB -c max_connections=20 -c unix_socket_directories=/tmp" \
    -w start

# Verify server readiness
pg_isready -h "$PGHOST" -p "$PGPORT" -U "$PGUSER"
echo "      [PASS] PostgreSQL server is ready and accepting connections."

# Step 3: Execute SQL demonstration workflow
echo "[3/5] Executing SQL operations inside isolated container..."

# Create database 'college'
echo "  -> Creating database 'college'..."
psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -v ON_ERROR_STOP=1 -d postgres -c "CREATE DATABASE college;"

# Connect to 'college', create table, insert records, and query
echo "  -> Creating 'students' table and inserting records..."
psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -v ON_ERROR_STOP=1 -d college << 'EOF'
CREATE TABLE students (
    id SERIAL PRIMARY KEY,
    name VARCHAR(50) NOT NULL,
    department VARCHAR(50) NOT NULL
);

INSERT INTO students (name, department) VALUES ('Rahul', 'Computer Science');
INSERT INTO students (name, department) VALUES ('Priya', 'Information Technology');
EOF

echo "  -> Querying students table:"
echo "------------------------------------------------------------"
psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -v ON_ERROR_STOP=1 -d college -c "SELECT * FROM students ORDER BY id;"
echo "------------------------------------------------------------"

# Step 4: Display connection and process metadata
echo "[4/5] Inspecting runtime process and connection metadata..."

POSTMASTER_PID=$(head -n 1 "$PGDATA/postmaster.pid" 2>/dev/null || echo "unknown")

psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -v ON_ERROR_STOP=1 -d college -A -F "|" -t << 'EOF' > /tmp/minic_pg_meta.txt
SELECT current_database(), inet_server_addr(), inet_server_port(), pg_backend_pid();
EOF

DB_NAME=$(cut -d'|' -f1 /tmp/minic_pg_meta.txt)
SRV_ADDR=$(cut -d'|' -f2 /tmp/minic_pg_meta.txt)
SRV_PORT=$(cut -d'|' -f3 /tmp/minic_pg_meta.txt)
BACKEND_PID=$(cut -d'|' -f4 /tmp/minic_pg_meta.txt)
rm -f /tmp/minic_pg_meta.txt

PG_VERSION_STR=$(psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -t -A -c "SELECT version();")

echo "  Database Name:       $DB_NAME"
echo "  Server Address:      $SRV_ADDR"
echo "  Server Port:         $SRV_PORT"
echo "  Postmaster Server PID: $POSTMASTER_PID"
echo "  Query Backend PID:   $BACKEND_PID"
echo "  In-Container Shell PID: $$"
echo "  Server Version:      $PG_VERSION_STR"
echo "  [PASS] All SQL operations and metadata inspections succeeded."

# Step 5: Readiness announcement & hold handling
echo "[5/5] Demonstration workflow complete."
echo "MINIC_POSTGRES_READY"

if [ "$HOLD_SECONDS" -gt 0 ]; then
    echo "[MiniC Demo] Holding container alive for ${HOLD_SECONDS}s..."
    echo "[MiniC Demo] Host port isolation test may now query 127.0.0.1:$PGPORT."
    REMAINING=$HOLD_SECONDS
    while [ "$REMAINING" -gt 0 ]; do
        sleep 1 &
        wait $! || true
        REMAINING=$((REMAINING - 1))
    done
    echo "[MiniC Demo] Hold duration elapsed. Exiting..."
fi
