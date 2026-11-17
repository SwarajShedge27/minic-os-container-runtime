# MiniC — Lightweight Linux Container Runtime from Scratch

MiniC is an educational Linux container runtime built from scratch in Go for an undergraduate Operating Systems project. It demonstrates core operating system concepts—namespaces (PID, UTS, Mount, Network), cgroups v2 resource limits, OverlayFS copy-on-write filesystems, `pivot_root`, privilege hardening with capability dropping and `no_new_privs`, and safe non-root execution—culminating in running an isolated, production-grade **PostgreSQL 17.12** database service.

---

## Developer Quick-Start & FAQ

### 1. What is MiniC?
MiniC is a lightweight, independent implementation of a Linux container runtime written in Go. It demonstrates how the Linux kernel isolates, constrains, and hardens processes using native kernel APIs rather than hardware virtualization.

### 2. Which OS concepts does it demonstrate?
- **Process Isolation:** Independent process tables via PID namespaces (`CLONE_NEWPID`).
- **Hostname Isolation:** Independent system hostnames via UTS namespaces (`CLONE_NEWUTS`).
- **Filesystem & Mount Isolation:** Private mount namespaces (`CLONE_NEWNS`), OverlayFS copy-on-write union filesystems, minimal character device provisioning, and root relocation via `pivot_root`.
- **Network Isolation:** Private network stack (`CLONE_NEWNET`) with isolated loopback interface activation via socket ioctls (`SIOCGIFFLAGS`/`SIOCSIFFLAGS`).
- **Resource Management:** Memory, CPU bandwidth, and PID limits via Linux cgroups v2 (`memory.max`, `cpu.max`, `pids.max`).
- **Process Synchronization & Lifecycle:** Pipe/socket-based parent-child synchronization, signal interception and forwarding (`SIGINT`, `SIGTERM`, `SIGKILL`), and exit code propagation.
- **Privilege Hardening:** OS thread pinning (`runtime.LockOSThread()`), capability bounding set dropping (`PR_CAPBSET_DROP`), ambient capabilities reset (`PR_CAP_AMBIENT_CLEAR_ALL`), `no_new_privs` enforcement (`PR_SET_NO_NEW_PRIVS`), and full capability clearing (`unix.Capset`).
- **Safe Non-Root Execution:** Unprivileged process execution via optional `--uid` and `--gid` flags with strict credential transition ordering (`setresgid` before `setresuid`).
- **Real-World Service Isolation:** Running a full database engine (PostgreSQL 17.12) under UID/GID 70 on Alpine Linux 3.24.4, isolated from host network sockets.

### 3. How is it different from a full container engine?
MiniC is a standalone educational runtime, not an enterprise container platform:
- It does **not** download Docker images, parse multi-layer OCI tarballs, or communicate with container registries.
- It operates directly on provided root filesystem directories (`rootfs` for BusyBox, or a prepared Alpine rootfs for PostgreSQL).
- It omits daemon architectures (`dockerd`, `containerd`), managing execution synchronously in the foreground.

### 4. Which Linux and Go prerequisites are required?
- **Operating System:** Linux with kernel $\ge 5.4$ or Windows 10/11 running **WSL 2 (Ubuntu)**.
- **Go Toolchain:** Go 1.24+ (project module uses Go 1.26).
- **Kernel Features:** cgroups v2 unified hierarchy mounted at `/sys/fs/cgroup`, OverlayFS kernel module enabled, and standard loopback networking support.

### 5. How do I build it?
Use the unprivileged Makefile target:
```bash
make build
```
This compiles the executable to `bin/minic` without touching the baseline root-level executable.

### 6. How do I launch the simplest BusyBox container?
```bash
sudo ./bin/minic run ./rootfs /bin/echo "Hello from MiniC!"
```

### 7. How do I apply memory, CPU and PID limits?
```bash
sudo ./bin/minic run \
  --memory 128M \
  --cpu 50 \
  --pids 20 \
  --hostname minic-demo \
  ./rootfs /bin/sh
```

### 8. How do I run PostgreSQL inside MiniC?
Use the automated convenience target:
```bash
make demo-postgres
```
Or execute manually with explicit parameters:
```bash
sudo ./bin/minic run \
  --rootfs "$HOME/.cache/minic/rootfs-postgres" \
  --uid 70 --gid 70 \
  --memory 256M --cpu 100 --pids 64 \
  --hostname minic-postgres \
  /usr/local/bin/minic-postgres-demo --hold-seconds 120
```

### 9. How do I run the test suite?
Run the standard unprivileged checks (unit tests, static analysis, dry compilation, and diff checks):
```bash
make check
```
Or run individual unprivileged targets:
```bash
make test   # Run all 99 unit tests
make vet    # Run Go static analysis
```

### 10. How do I run the privileged integration tests safely?
MiniC includes five privileged integration test scripts that execute on disposable root filesystems without altering the host environment or original project files:
```bash
make integration-cow        # Test OverlayFS copy-on-write, device mounts & cleanup
make integration-net        # Test Network namespace isolation & loopback IPC
make integration-security   # Test Capability dropping, NoNewPrivs & setuid protection
make integration-cgroup     # Test Cgroups v2 memory, CPU, and PID live enforcement
make integration-postgres   # Test PostgreSQL rootfs, non-root execution, SQL & network isolation
make integration            # Run all integration suites sequentially
```

### 11. Which features remain unsupported?
- **User Namespaces (`CLONE_NEWUSER`):** Container processes run with real host UIDs (e.g. UID 0 or UID 70). Virtual UID-to-host-UID mapping is not implemented.
- **Seccomp Syscall Filtering:** BPF syscall filters are not attached.
- **Virtual Ethernet & External Networking:** No `veth` pair bridging, NAT, port forwarding, or internet access (loopback-only).
- **Mandatory Access Control (MAC):** AppArmor and SELinux profiles are omitted.
- **OCI Specification:** Does not parse `config.json` bundles.

---

## Core Architecture & Execution Sequence

```mermaid
flowchart TD
    A[User Command: minic run ...] --> B[CLI Parsing & Flag Validation]
    B --> C[Parent Container Manager]
    C --> D[Stage Rootfs to Linux-Native /tmp/minic-run-*]
    D --> E[Create Anonymous Synchronization Pipe]
    E --> F[Spawn Re-Exec Child with CLONE_NEWPID | CLONE_NEWUTS | CLONE_NEWNS | CLONE_NEWNET]

    F --> G[Child Blocks Reading from Synchronization Pipe]
    C --> H[Extract Child Host PID]
    H --> I[Create cgroups v2 Directory: /sys/fs/cgroup/minic-pid]
    I --> J[Apply Limits: memory.max, cpu.max, pids.max]
    J --> K[Write Child Host PID to cgroup.procs]
    K --> L[Parent Writes Unlock Token to Sync Pipe]

    L --> G
    G --> M[Child Resumes & Closes Sync Pipe Descriptor]
    M --> N[Child: Set Mount Propagation to MS_PRIVATE]
    N --> O[Child: Mount OverlayFS lower/upper/work into merged]
    O --> P[Child: Bind Mount merged to itself]
    P --> Q[Child: Bind Mount /dev/null, zero, random, urandom]
    Q --> R[Child: pivot_root into merged & Detach Old Host Root]
    R --> S[Child: Mount virtual /proc]
    S --> T[Child: Configure lo Interface to UP via socket ioctls]
    T --> U[Child: Set Hostname via sethostname]
    U --> V[Child: If --gid/--uid set, call setresgid and setresuid]
    V --> W[Child: Lock OS Thread via runtime.LockOSThread]
    W --> X[Child: Drop Bounding Set Caps PR_CAPBSET_DROP]
    X --> Y[Child: Clear Ambient Caps PR_CAP_AMBIENT_CLEAR_ALL]
    Y --> Z[Child: Enable no_new_privs PR_SET_NO_NEW_PRIVS]
    Z --> AA[Child: Zero Effective, Permitted, Inheritable Caps unix.Capset]
    AA --> AB[Child: unix.Exec Application Image]

    AB --> AC[Parent Waits, Intercepts & Forwards SIGINT/SIGTERM]
    AC --> AD[Process Terminates: Parent Captures Exit Code]
    AD --> AE[Parent Cleanup: Kill lingering tasks & Remove cgroup]
    AE --> AF[Parent Cleanup: Unmount & Erase Staging Directory]
    AF --> AG[Parent Exits with Preserved Exit Code]
```

### Component Relationships
- **Namespaces:** Activated simultaneously at `clone()` time. PID isolation gives the child PID 1; Mount isolation allows private mounts; UTS isolation sandboxes hostname changes; Network isolation hides host network interfaces.
- **cgroups v2:** Configured strictly on the host by the parent. The child is paused on an anonymous pipe until registered into `cgroup.procs`, ensuring resource limits apply from instruction zero.
- **OverlayFS:** Bridges the immutable base rootfs (`lowerdir`) with an ephemeral staging directory (`upperdir`/`workdir`). Container mutations are isolated in `upperdir` and unlinked on teardown.
- **pivot_root & `/proc`:** Replaces the host mount point with the merged OverlayFS, unmounts the old host root via `MNT_DETACH`, and mounts a container-local `/proc`.
- **Privilege Hardening:** Strips all 5 capability sets and enables `no_new_privs` immediately before `execve`, ensuring the application cannot perform privileged syscalls.
- **Non-Root Execution:** When `--uid` and `--gid` are supplied, MiniC transitions GID followed by UID after mount/network configuration, allowing complex multi-process services like PostgreSQL to run under dedicated service accounts.
- **Cleanup:** Defers guarantee LIFO order: processes killed, cgroup removed, and temporary files unlinked upon exit or signal receipt.

---

## Comparison with runc

| Feature                      | MiniC                         | runc                         |
| ---------------------------- | ----------------------------- | ---------------------------- |
| Linux namespaces             | Yes (PID, UTS, Mount, Net)    | Yes (All namespaces)         |
| cgroups v2                   | Yes (CPU, Memory, PIDs)       | Yes (Full controller set)    |
| Rootfs isolation             | Yes (`pivot_root` + private)  | Yes                          |
| OverlayFS copy-on-write      | Yes (Disposable staging)      | Yes (Layered drivers)        |
| Device nodes                 | Minimal (/dev/null, zero, etc)| Configurable / Full /dev     |
| Loopback interface           | Yes (Automatic `lo` UP)       | Configurable / Pre-start hook|
| Linux Capabilities           | Yes (Drops all bounding/eff)  | Configurable capability sets |
| no_new_privs                 | Yes (Always enabled)          | Configurable                 |
| Non-root execution           | Yes (`--uid`, `--gid`)        | Yes (`user` in OCI spec)     |
| Signal forwarding            | Yes (Graceful escalation)     | Yes                          |
| Exit code preservation       | Yes (Exact + 128+N signals)   | Yes                          |
| Real application showcase    | Yes (PostgreSQL 17.12)        | Yes (Any Linux payload)      |
| CLI                          | Simplified (`run`, `child`)   | Full OCI runtime interface   |
| OCI specification            | No                            | Yes                          |
| Docker image handling        | No                            | No, handled by higher layers |
| User namespaces              | No (Host UIDs)                | Yes                          |
| Seccomp syscall filtering    | No                            | Yes (BPF filters)            |
| Goal                         | Educational OS Demonstration  | Production container runtime |

---

## Feature & Verification Matrix

| Capability / Feature | Unit Tested | Privileged Integration Tested | Status |
| :--- | :---: | :---: | :---: |
| PID Namespace (`CLONE_NEWPID`) | Yes | Yes (`test/integration_net_test.sh`) | **VERIFIED** |
| UTS Namespace (`CLONE_NEWUTS`) | Yes | Yes (`test/integration_sec_test.sh`) | **VERIFIED** |
| Mount Namespace (`CLONE_NEWNS`) | Yes | Yes (`test/integration_cow_test.sh`) | **VERIFIED** |
| Network Namespace (`CLONE_NEWNET`) | Yes | Yes (`test/integration_net_test.sh`) | **VERIFIED** |
| Memory Limits (`memory.max`) | Yes | Yes (`test/integration_cgroup_test.sh`) | **VERIFIED** |
| CPU Limits (`cpu.max`) | Yes | Yes (`test/integration_cgroup_test.sh`) | **VERIFIED** |
| PID Limits (`pids.max`) | Yes | Yes (`test/integration_cgroup_test.sh`) | **VERIFIED** |
| Parent-Child Pipe Sync | Yes | Yes (`test/integration_cow_test.sh`) | **VERIFIED** |
| Signal Forwarding & Escalation | Yes | Yes (`test/integration_cow_test.sh`) | **VERIFIED** |
| Exit Code Preservation | Yes | Yes (`test/integration_cow_test.sh`) | **VERIFIED** |
| OverlayFS Copy-on-Write | Yes | Yes (`test/integration_cow_test.sh`) | **VERIFIED** |
| WSL Native Staging (`/tmp`) | Yes | Yes (`test/integration_cow_test.sh`) | **VERIFIED** |
| Device Provisioning (`/dev/*`) | Yes | Yes (`test/integration_cow_test.sh`) | **VERIFIED** |
| `pivot_root` & Old Root Detach | Yes | Yes (`test/integration_cow_test.sh`) | **VERIFIED** |
| Virtual `/proc` Mount | Yes | Yes (`test/integration_cow_test.sh`) | **VERIFIED** |
| Loopback Interface Activation | Yes | Yes (`test/integration_net_test.sh`) | **VERIFIED** |
| Unprivileged Loopback TCP IPC | Yes | Yes (`test/integration_net_test.sh`) | **VERIFIED** |
| Capability Bounding Set Drop | Yes | Yes (`test/integration_sec_test.sh`) | **VERIFIED** |
| Ambient Capabilities Reset | Yes | Yes (`test/integration_sec_test.sh`) | **VERIFIED** |
| `no_new_privs` Flag Enforced | Yes | Yes (`test/integration_sec_test.sh`) | **VERIFIED** |
| Effective/Permitted/Inheritable Cleared | Yes | Yes (`test/integration_sec_test.sh`) | **VERIFIED** |
| Setuid Binary Escalation Blocked | Yes | Yes (`test/integration_sec_test.sh`) | **VERIFIED** |
| Non-Root Execution (`--uid`/`--gid`)| Yes | Yes (`test/integration_postgres_test.sh`) | **VERIFIED** |
| Rootfs Ownership Preservation | Yes | Yes (`test/integration_postgres_test.sh`) | **VERIFIED** |
| PostgreSQL 17 Service Execution | — | Yes (`test/integration_postgres_test.sh`) | **VERIFIED** |
| Host Network Port Isolation | — | Yes (`test/integration_postgres_test.sh`) | **VERIFIED** |
| User Namespaces (`CLONE_NEWUSER`) | — | — | **UNSUPPORTED** |
| Seccomp BPF Syscall Filtering | — | — | **UNSUPPORTED** |
| External veth / NAT Bridging | — | — | **UNSUPPORTED** |

---

## Project Structure
```text
minic/
├── .github/
│   └── workflows/
│       └── ci.yml                     # GitHub Actions unprivileged CI workflow
├── cmd/
│   └── minic/
│       └── main.go                    # CLI main entry point
├── internal/
│   ├── cgroup/                      # cgroups v2 limits and lifecycle cleanup
│   ├── cli/                         # Argument parsing, usage, and exit codes
│   ├── container/                   # Parent container manager & signal handling
│   ├── filesystem/                  # Rootfs staging, OverlayFS mount, devices & pivot_root
│   ├── network/                     # Network namespace & loopback ioctl configuration
│   ├── process/                     # Child process initialization & namespace bootstrap
│   └── security/                    # Capability dropping and no_new_privs hardening
├── test/
│   ├── integration_cow_test.sh        # Privileged OverlayFS copy-on-write integration test
│   ├── integration_net_test.sh        # Privileged Network isolation integration test
│   ├── integration_sec_test.sh        # Privileged Security hardening & capability integration test
│   ├── integration_cgroup_test.sh     # Privileged Cgroups v2 live enforcement integration test
│   └── integration_postgres_test.sh   # Privileged PostgreSQL service integration test
├── scripts/
│   ├── prepare_postgres_rootfs.sh    # Alpine 3.24.4 & PostgreSQL 17 rootfs builder
│   └── postgres_container_demo.sh    # In-container PostgreSQL init, query & hold script
├── docs/
│   ├── final-presentation-guide.md    # Master presentation & viva examination guide (24 Q&As)
│   ├── postgres-demo-guide.md         # Dedicated PostgreSQL demonstration & troubleshooting guide
│   ├── vscode-run-and-demo-guide.md   # VS Code workspace setup & demonstration guide
│   ├── demo-guide.md                # 7-part BusyBox live demonstration guide
│   ├── viva-questions.md            # Foundational viva questions and answers
│   ├── presentation-notes.md        # 5-minute presentation script outline
│   └── minic-architecture.md        # Architectural design specifications
├── rootfs/                          # Bundled minimal BusyBox root filesystem (lowerdir)
├── Makefile                         # Unprivileged developer automation & test targets
├── go.mod                           # Go module definition
├── AGENTS.md                        # Developer guidelines
└── README.md                        # Master project documentation
```

---

## Demonstration & Viva Resources
- **Master Presentation & Viva Guide:** See [docs/final-presentation-guide.md](docs/final-presentation-guide.md) for the complete 5-minute presentation pitch, step-by-step commands, architectural diagrams, implementation deep dives, limitations, and 24 detailed viva examination questions with answers.
- **PostgreSQL Service Demonstration:** See [docs/postgres-demo-guide.md](docs/postgres-demo-guide.md) for the dedicated real-world database demonstration, two-terminal network isolation test, and storage architecture details.
- **VS Code Run & Demo Guide:** See [docs/vscode-run-and-demo-guide.md](docs/vscode-run-and-demo-guide.md) for opening the project in VS Code and interactive testing.
- **Basic BusyBox Demo Guide:** See [docs/demo-guide.md](docs/demo-guide.md) for the 7-part BusyBox demonstration sequence.
