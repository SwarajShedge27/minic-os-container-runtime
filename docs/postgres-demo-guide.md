# MiniC — PostgreSQL Container Demonstration Guide

This guide describes how to run and demonstrate an enterprise-grade service—**PostgreSQL 17.12** on **Alpine Linux 3.24.4**—inside the **MiniC** lightweight Linux container runtime.

---

## 1. Overview & Architecture

MiniC is an educational Linux container runtime written in Go. Rather than relying on simple BusyBox shell scripts alone, this demonstration proves MiniC can isolate, resource-control, and safely execute a complex production database service without Docker or third-party container engines.

```
+-------------------------------------------------------------------------+
| HOST (WSL 2 Ubuntu / Linux)                                             |
|                                                                         |
|  [MiniC Parent Runtime] (CLI parser, cgroup setup, network isolation)   |
|         │                                                               |
|         ▼ clone with CLONE_NEWPID, CLONE_NEWNS, CLONE_NEWUTS, NEWNET    |
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
|  |         ├── initdb (/tmp/minic-pgdata on OverlayFS writable layer)|  |
|  |         ├── postmaster (pg_ctl -p 15432 -h 127.0.0.1)             |  |
|  |         └── psql client (CREATE DB, TABLE, INSERT, SELECT)        |  |
|  +-------------------------------------------------------------------+  |
|         │                                                               |
|  Host port 15432 remains completely CLOSED / UNREACHABLE                |
+-------------------------------------------------------------------------+
```

---

## 2. Prerequisites & Environment Setup

Before presenting or running the PostgreSQL container:

1. **WSL 2 Ubuntu Environment**: Linux kernel 5.15+ with cgroups v2 unified hierarchy enabled (`/sys/fs/cgroup/cgroup.controllers`).
2. **MiniC Repository Location**: Cloned on local storage.
3. **Compiled Executable**: `bin/minic` built using `make build`.
4. **PostgreSQL Rootfs Location**: Native Linux ext4 directory (default: `$HOME/.cache/minic/rootfs-postgres`).
5. **Sudo Privileges**: Required to invoke Linux namespace syscalls (`clone`), configure cgroups v2, and mount OverlayFS layers.

---

## 3. Preparation Commands

Run these steps in order to ensure the environment is ready for demonstration:

### Step 1: Navigate to MiniC Project Directory
```bash
cd "/mnt/c/Users/Swaraj Shedge/Desktop/College/TY/OS/OS Project/minic"
```

### Step 2: Build MiniC Binary
```bash
make build
```
*Output: `Build complete: bin/minic`*

### Step 3: Verify PostgreSQL Rootfs & Installed Version
If not already prepared, build the rootfs on native storage:
```bash
sudo ./scripts/prepare_postgres_rootfs.sh
```
Verify the installed PostgreSQL version inside the rootfs:
```bash
/home/swaraj_shedge/.cache/minic/rootfs-postgres/usr/bin/postgres --version
# Output: postgres (PostgreSQL) 17.12
```

### Step 4: Verify Host Port 15432 is Free
Before launching the container, verify that host port 15432 is not occupied:
```bash
python3 -c "import socket; s = socket.socket(); res = s.connect_ex(('127.0.0.1', 15432)); s.close(); print('Port 15432 is free' if res != 0 else 'WARNING: Port in use!')"
```
*Must output: `Port 15432 is free`.*

---

## 4. Demonstration Mode 1: Automated Foreground Run

This mode executes the full database lifecycle from startup to shutdown in a single terminal.

### Run Command
```bash
sudo ./bin/minic run \
    --memory 256M \
    --cpu 100 \
    --pids 64 \
    --uid 70 \
    --gid 70 \
    --hostname minic-postgres \
    /home/swaraj_shedge/.cache/minic/rootfs-postgres \
    /usr/local/bin/minic-postgres-demo
```

### Observed Execution Walkthrough
1. **Container Bootstrap**: MiniC creates new PID, Mount, UTS, and Network namespaces. It applies cgroups v2 resource limits (256MB memory, 100% CPU quota, 64 PIDs max) and sets container hostname to `minic-postgres`.
2. **Privilege Dropping**: Credentials change to UID 70 / GID 70 (`postgres`). Capabilities are zeroed, and `no_new_privs` is locked.
3. **Cluster Initialization (`initdb`)**: Ephemeral directory `/tmp/minic-pgdata` is created on the container's OverlayFS writable layer and initialized with UTF-8 encoding.
4. **Server Startup (`pg_ctl`)**: PostgreSQL starts, listening strictly on container loopback `127.0.0.1:15432`.
5. **SQL Operations (`psql`)**:
   - `CREATE DATABASE college;`
   - `CREATE TABLE students (id SERIAL PRIMARY KEY, name VARCHAR(50), department VARCHAR(50));`
   - Inserts records for Rahul (Computer Science) and Priya (Information Technology).
   - Executes `SELECT * FROM students ORDER BY id;`.
6. **Metadata Introspection**: Queries PostgreSQL version, database name, port, server address, backend PID, and postmaster PID.
7. **Clean Shutdown**: `pg_ctl -m fast stop` halts the server. Ephemeral storage is deleted. Container exits cleanly.

### Expected Terminal Output
```
============================================================
      MiniC PostgreSQL 17 Container Demonstration
============================================================
Container Hostname: minic-postgres
Process User:       uid=70(postgres) gid=70(postgres)
Hold Duration:      0s
Data Directory:     /tmp/minic-pgdata (ephemeral container storage - OverlayFS writable layer)
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
  Database Name:         college
  Server Address:        127.0.0.1
  Server Port:           15432
  Postmaster Server PID: 20
  Query Backend PID:     38
  In-Container Shell PID: 1
  Server Version:        PostgreSQL 17.12 on x86_64-alpine-linux-musl, compiled by cc (Alpine 15.2.0) 15.2.0, 64-bit
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

## 5. Demonstration Mode 2: Live Hold & Network Isolation Demo

This mode demonstrates true Linux network namespace isolation. PostgreSQL runs and holds inside MiniC; while it actively accepts connections on its own loopback interface (`127.0.0.1:15432`), the host machine's loopback interface cannot reach it.

### Step 1: Terminal 1 — Start MiniC with Hold
You can use the convenient Makefile target:
```bash
make demo-postgres
```
Or run the direct CLI command:
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

Wait until Terminal 1 outputs:
```
MINIC_POSTGRES_READY
[MiniC Demo] Holding container alive for 120s...
[MiniC Demo] Host port isolation test may now query 127.0.0.1:15432.
```

### Step 2: Terminal 2 — Test Host Network Isolation
While Terminal 1 is holding, switch to Terminal 2 and attempt to connect to port 15432 from the host:

```bash
python3 -c "import socket; s = socket.socket(); s.connect(('127.0.0.1', 15432))"
```

**Observed Result:**
```
ConnectionRefusedError: [Errno 111] Connection refused
```

### OS Explanation for Evaluators
- **Network Namespace (`CLONE_NEWNET`)**: The container has its own private loopback interface (`lo`), socket table, and routing table.
- PostgreSQL binds to `127.0.0.1:15432` **inside** the container namespace.
- The host network namespace has no process listening on port 15432.
- The connection is actively refused, proving network isolation without firewall rules or iptables hacks.

---

## 6. Filesystem Architecture & Storage Claims

An essential technical distinction verified via `/proc/self/mountinfo`:

```
565 333 0:89 / / rw,relatime - overlay overlay rw,lowerdir=.../lower,upperdir=.../upper,workdir=.../work
463 565 0:91 / /proc rw,relatime - proc proc rw
```

- **Not a separate `tmpfs`**: `/tmp/minic-pgdata` is created within the container root directory `/`, which is an OverlayFS merged view.
- **OverlayFS Writable Layer**: All database files, transaction logs (WAL), and temporary sockets written to `/tmp/minic-pgdata` reside on the **ephemeral OverlayFS upper directory (`upperdir`)**.
- **Complete Host Protection**: The base image in the lower directory (`lowerdir`) is opened read-only.
- **Teardown**: When the container process terminates, MiniC unmounts OverlayFS and deletes `upperdir` and `workdir`. All database modifications evaporate.

---

## 7. Automated Integration Test

To verify PostgreSQL container startup, SQL operations, network isolation, and resource cleanup programmatically:

```bash
make integration-postgres
```

This runs `test/integration_postgres_test.sh`, which asserts:
1. Host port 15432 is free.
2. Container starts in background with `--hold-seconds 12`.
3. `MINIC_POSTGRES_READY` is announced.
4. Host connection attempt to 127.0.0.1:15432 is refused.
5. In-container log verifies SQL table queries and records.
6. Container shuts down cleanly on hold completion.
7. Original BusyBox rootfs is byte-for-byte unchanged.
8. All mounts, staging dirs, cgroups, and processes are cleaned up.

---

## 8. Troubleshooting Guide

| Issue | Symptom | Cause | Solution |
| :--- | :--- | :--- | :--- |
| **Port Occupied** | `ERROR: Host TCP port 15432 is already occupied` | Another service is using port 15432 on the host. | Run `lsof -i :15432` or `ss -tulpn \| grep 15432` and stop the conflicting process. |
| **Rootfs Missing** | `ERROR: PostgreSQL rootfs not found` | The rootfs builder script has not been executed. | Run `sudo ./scripts/prepare_postgres_rootfs.sh`. |
| **Permission Denied** | `initdb: error: could not change permissions of directory` | Staging or upperdir created without write permissions. | Verify the rootfs was prepared on native Linux storage (not `/mnt/c`). |
| **PostgreSQL Startup Fail** | `pg_ctl: could not start server` | Shared memory or connection limit exhaustion. | Check `/tmp/postgres.log`. Ensure MiniC is run with `--memory 256M`. |
| **Cgroup Limit Exceeded** | `/bin/sh: can't fork` or container OOM-killed (137) | Limits too restrictive for PostgreSQL 17. | Use recommended demo limits: `--memory 256M --pids 64`. |
| **Shutdown Timeout** | `pg_ctl: server did not stop in time` | Client backend process hung. | Script automatically falls back to `pg_ctl -m immediate stop`. |

---

## 9. Technical Limitations

1. **Foreground Execution Only**: MiniC is a foreground container runner. Background daemonization (`-d`) is outside the educational project scope.
2. **No Attach / Exec Interface**: There is no `minic exec` command. All demonstration logic is executed via the in-container entrypoint script.
3. **Ephemeral State**: All database data is temporary and discarded upon container exit. Persistent volumes are not implemented in this phase.
4. **Trust Authentication**: PostgreSQL uses `--auth=trust` solely for isolated, offline student demonstration. Production systems require password/PAM/certificate authentication.
