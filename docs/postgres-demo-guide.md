# MiniC — PostgreSQL Container Demonstration Guide

This guide describes how to run and demonstrate an enterprise-grade service—PostgreSQL 17.12—inside the **MiniC** lightweight Linux container runtime.

---

## 1. Overview & Architecture

MiniC is an educational Linux container runtime written in Go. Rather than relying on BusyBox shell scripts alone, this demonstration proves MiniC can isolate and execute a production database service.

```
+-------------------------------------------------------------------------+
| HOST (Linux / WSL 2 Ubuntu)                                             |
|                                                                         |
|  [MiniC Parent Process] (CLI parser, cgroup setup, network isolation)   |
|         │                                                               |
|         ▼ fork/clone with CLONE_NEWPID, NEWNS, NEWUTS, NEWNET           |
|  +-------------------------------------------------------------------+  |
|  | MINIC CONTAINER ENVIRONMENT                                       |  |
|  |                                                                   |  |
|  |  • Hostname:        minic-postgres                                |  |
|  |  • Rootfs:          Alpine 3.24.4 (OverlayFS copy-on-write)       |  |
|  |  • Credentials:     UID 70, GID 70 (postgres:postgres)            |  |
|  |  • Capabilities:    All zeroed (CapEff, CapPrm, CapInh, CapBnd=0) |  |
|  |  • Security:        NoNewPrivs = 1 (setuid escalation barred)     |  |
|  |  • Network:         Isolated netns (Loopback 127.0.0.1:15432)     |  |
|  |  • Resources:       Memory: 256MB | CPU: 100% | PIDs: 64          |  |
|  |                                                                   |  |
|  |  [In-Container Process: /usr/local/bin/minic-postgres-demo]       |  |
|  |         │                                                         |  |
|  |         ├── initdb (/tmp/minic-pgdata)                            |  |
|  |         ├── postmaster (pg_ctl -p 15432 -h 127.0.0.1)             |  |
|  |         └── psql client (CREATE DB, TABLE, INSERT, SELECT)        |  |
|  +-------------------------------------------------------------------+  |
|         │                                                               |
|  Host port 15432 remains completely CLOSED / UNREACHABLE                |
+-------------------------------------------------------------------------+
```

---

## 2. Prerequisites & Rootfs Preparation

The PostgreSQL container requires a native Linux root filesystem. The preparation script automatically downloads the official pinned Alpine Linux minirootfs (`v3.24.4`), verifies its SHA-256 checksum, and installs PostgreSQL 17.12 via Alpine's native package manager (`apk`) inside a chroot.

### Build the PostgreSQL Rootfs

```bash
# Execute as root (or with sudo)
sudo ./scripts/prepare_postgres_rootfs.sh
```

**What this script does:**
1. Verifies the host architecture is `x86_64`.
2. Confirms target storage is on native Linux ext4/tmpfs (actively rejects Windows DrvFs `/mnt/c`).
3. Downloads `alpine-minirootfs-3.24.4-x86_64.tar.gz` and validates SHA-256 (`cdaa38cb8de...`).
4. Installs `postgresql17=17.12-r0`, `postgresql17-client`, and `postgresql17-contrib`.
5. Installs the orchestration script `/usr/local/bin/minic-postgres-demo`.
6. Configures user `postgres` (UID 70, GID 70), directory permissions (`/var/lib/postgresql` 70:70, `/run/postgresql` 70:70, `/tmp` 1777).
7. Validates binaries (`postgres`, `initdb`, `pg_ctl`, `psql`, `pg_isready`) and dynamic libraries.

Target directory: `$HOME/.cache/minic/rootfs-postgres`.

---

## 3. Demonstration Mode 1: Automated Foreground Run

Run the complete database lifecycle (cluster initialization, server startup, database creation, table creation, record insertion, SQL querying, connection metadata inspection, and graceful fast shutdown).

### Build MiniC Binary

```bash
make build
```

### Run Command

```bash
sudo ./bin/minic run \
    --memory 256M \
    --cpu 100 \
    --pids 64 \
    --uid 70 \
    --gid 70 \
    --hostname minic-postgres \
    "$HOME/.cache/minic/rootfs-postgres" \
    /usr/local/bin/minic-postgres-demo
```

*(Note: If run as root, replace `$HOME` with the actual home directory, e.g. `/home/swaraj_shedge/.cache/minic/rootfs-postgres`).*

### Expected Output

```
============================================================
      MiniC PostgreSQL 17 Container Demonstration
============================================================
Container Hostname: minic-postgres
Process User:       uid=70(postgres) gid=70(postgres)
Hold Duration:      0s
Data Directory:     /tmp/minic-pgdata (ephemeral tmpfs)
============================================================
[1/5] Initializing database cluster with initdb...
      [PASS] Cluster initialized at /tmp/minic-pgdata (UTF-8, superuser: postgres).
[2/5] Starting PostgreSQL server on 127.0.0.1:15432...
waiting for server to start.... done
server started
127.0.0.1:15432 - accepting connections
      [PASS] PostgreSQL server is ready and accepting connections.
[3/5] Executing SQL operations inside isolated container...
  -> Creating database 'college'...
CREATE DATABASE
  -> Creating 'students' table and inserting records...
CREATE TABLE
INSERT 0 1
INSERT 0 1
  -> Querying students table:
------------------------------------------------------------
 id | name  |       department       
----+-------+------------------------
  1 | Rahul | Computer Science
  2 | Priya | Information Technology
(2 rows)

------------------------------------------------------------
[4/5] Inspecting runtime process and connection metadata...
  Database Name:       college
  Server Address:      127.0.0.1
  Server Port:         15432
  Postmaster Server PID: 20
  Query Backend PID:   38
  In-Container Shell PID: 1
  Server Version:      PostgreSQL 17.12 on x86_64-alpine-linux-musl...
  [PASS] All SQL operations and metadata inspections succeeded.
[5/5] Demonstration workflow complete.
MINIC_POSTGRES_READY

[MiniC Demo] Initiating graceful database shutdown...
waiting for server to shut down.... done
server stopped
[MiniC Demo] [PASS] PostgreSQL stopped cleanly via pg_ctl.
[MiniC Demo] [PASS] Ephemeral database storage cleaned up.
```

---

## 4. Demonstration Mode 2: Live Hold & Network Isolation Demo

This mode demonstrates true Linux network namespace isolation. PostgreSQL runs and holds inside MiniC; while it actively accepts connections on its own loopback interface (`127.0.0.1:15432`), the host machine's loopback interface cannot reach it.

### Step 1: In Terminal 1 — Start MiniC with Hold

```bash
sudo ./bin/minic run \
    --memory 256M \
    --cpu 100 \
    --pids 64 \
    --uid 70 \
    --gid 70 \
    --hostname minic-postgres \
    /home/swaraj_shedge/.cache/minic/rootfs-postgres \
    /usr/local/bin/minic-postgres-demo --hold-seconds 120
```

Wait until Terminal 1 prints:
```
MINIC_POSTGRES_READY
[MiniC Demo] Holding container alive for 120s...
[MiniC Demo] Host port isolation test may now query 127.0.0.1:15432.
```

### Step 2: In Terminal 2 — Test Host Network Isolation

While Terminal 1 is holding, test whether the host can reach port 15432:

```bash
# Using nc (netcat)
nc -zv 127.0.0.1 15432
# Output: (UNKNOWN) [127.0.0.1] 15432: Connection refused

# Or using Python socket check:
python3 -c "import socket; s = socket.socket(); s.connect(('127.0.0.1', 15432))"
# Output: ConnectionRefusedError: [Errno 111] Connection refused
```

### Why Does This Happen? (OS Concept Explanation)

- **Network Namespace Isolation (`CLONE_NEWNET`)**: MiniC assigns the container a completely private network stack, interface table, and routing table.
- The in-container PostgreSQL process binds exclusively to the container's private `lo` interface at `127.0.0.1:15432`.
- The host's `lo` interface has no listener on port 15432. Hence, TCP packets from the host never reach the container.

---

## 5. Non-Root Execution Details

- **PostgreSQL Requirement**: By design, PostgreSQL rejects running as UID 0 (`root`) to prevent arbitrary code execution with kernel/system privileges.
- **MiniC Implementation**:
  - The runtime accepts `--uid 70` and `--gid 70`.
  - In `childProcessInit()`, MiniC drops Linux capabilities first, enables `PR_SET_NO_NEW_PRIVS`, clears supplementary groups via `setgroups(nil)`, and sets unprivileged credentials using `setresgid` and `setresuid`.
  - Directory ownership preservation (`internal/filesystem/copy.go`) uses `os.Lchown` to preserve UID/GID 70 across the OverlayFS copy-on-write upperdir without requiring root within the container.

---

## 6. Ephemeral Storage Details

- **Zero Host Mutation**: All container files reside in a temporary OverlayFS staging directory under `/tmp/minic-run-*`.
- **Ephemeral Cluster**: The database cluster `/tmp/minic-pgdata` is created on the container's tmpfs mount during demo execution.
- **Immediate Cleanup**: Upon container exit (or signal interruption), MiniC unmounts all OverlayFS layers, deletes the staging directory, and removes cgroup controllers. No database files or postmaster sockets persist on the host.

---

## 7. Automated Integration Test

To run the complete automated test suite verifying PostgreSQL startup, SQL execution, network isolation, and resource cleanup:

```bash
make integration-postgres
```

Output:
```
=== MiniC Phase 6.3: PostgreSQL Integration & Network Isolation Test ===
[1/7] Building fresh MiniC binary...
[2/7] Verifying host TCP port 15432 is not in use...
  -> Assertion PASS: Host port 15432 is free.
[3/7] Launching PostgreSQL container in isolated network namespace...
  -> MiniC launched (PID 481). Waiting for MINIC_POSTGRES_READY marker...
  -> Marker detected: PostgreSQL is running and holding inside container.
[4/7] Testing host network isolation against in-container PostgreSQL...
  -> Assertion PASS: Host connection to 127.0.0.1:15432 actively refused / unreachable.
[5/7] Verifying in-container SQL operations and metadata...
  -> Assertion PASS: In-container server accepted connections on 127.0.0.1:15432.
  -> Assertion PASS: Table records (Rahul, Priya) verified in SQL query output.
  -> Assertion PASS: Database 'college' verified in connection metadata.
[6/7] Waiting for container hold duration to elapse and clean shutdown...
  -> Assertion PASS: Clean pg_ctl database shutdown confirmed.
[7/7] Verifying host resource cleanup and source rootfs integrity...
  -> Assertion PASS: Original BusyBox rootfs is 100% pristine and untouched.
  -> Assertion PASS: No lingering OverlayFS mounts.
  -> Assertion PASS: All temporary staging directories cleaned up.
  -> Assertion PASS: All cgroups v2 control groups removed.
  -> Assertion PASS: No orphan PostgreSQL server processes.
=== All PostgreSQL Integration & Network Isolation Tests Passed! ===
```

---

## 8. Honest Limitations

1. **No Background / Detached Mode**: MiniC operates as a foreground lifecycle manager. Daemonized (`-d`) execution is outside current project scope.
2. **No Attach / Exec Interface**: There is no secondary `minic exec` command. All in-container orchestration is driven through the entrypoint script.
3. **Ephemeral Storage Only**: Data written to the database is discarded when the container exits. Persistent host bind mounts or named volume management are not implemented.
4. **Trust Authentication**: PostgreSQL is initialized with `--auth=trust` for seamless offline demonstration in an isolated namespace; production deployments would require password or certificate authentication.
