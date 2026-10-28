# MiniC — Lightweight Linux Container Runtime from Scratch

MiniC is an educational Linux container runtime built from scratch in Go. It demonstrates core operating system concepts—namespaces (PID, UTS, Mount, Network), cgroups v2 resource limits, OverlayFS copy-on-write filesystems, `pivot_root`, and Linux capability dropping with `no_new_privs`—without the production-grade overhead of tools like Docker or `runc`.

---

## Developer Quick-Start & FAQ

### 1. What is MiniC?
MiniC is a lightweight, independent implementation of a Linux container runtime written in Go for an undergraduate Operating Systems project. It demonstrates how the Linux kernel isolates and constrains processes using native kernel APIs rather than virtualization.

### 2. Which OS concepts does it demonstrate?
- **Process Isolation:** Independent process tables via PID namespaces (`CLONE_NEWPID`).
- **Hostname Isolation:** Independent system hostnames via UTS namespaces (`CLONE_NEWUTS`).
- **Filesystem & Mount Isolation:** Private mount namespaces (`CLONE_NEWNS`), OverlayFS copy-on-write union filesystems, minimal character device provisioning, and root relocation via `pivot_root`.
- **Network Isolation:** Private network stack (`CLONE_NEWNET`) with isolated loopback interface activation via socket ioctls (`SIOCGIFFLAGS`/`SIOCSIFFLAGS`).
- **Resource Management:** Memory, CPU bandwidth, and PID limits via Linux cgroups v2 (`memory.max`, `cpu.max`, `pids.max`).
- **Process Synchronization & Lifecycle:** Pipe-based parent-child synchronization, signal interception and forwarding (`SIGINT`, `SIGTERM`, `SIGKILL`), and exit code propagation.
- **Privilege Hardening:** OS thread pinning (`runtime.LockOSThread()`), capability bounding set dropping (`PR_CAPBSET_DROP`), ambient capabilities reset (`PR_CAP_AMBIENT_CLEAR_ALL`), `no_new_privs` enforcement (`PR_SET_NO_NEW_PRIVS`), and full capability clearing (`unix.Capset`).

### 3. How is it different from a full container engine?
MiniC is a standalone runtime, not a full platform:
- It does **not** download Docker images, parse multi-layer tarballs, or talk to OCI registries.
- It operates directly on a provided directory structure (`rootfs`) bundled with minimal BusyBox binaries.
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

### 6. How do I launch the simplest command?
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

### 8. How do I run the test suite?
Run the standard unprivileged checks (unit tests, static analysis, dry compilation, and diff checks):
```bash
make check
```
Or run individual unprivileged targets:
```bash
make test   # Run all 99 unit tests
make vet    # Run Go static analysis
```

### 9. How do I run the privileged integration tests safely?
MiniC includes three privileged integration test scripts that execute on disposable root filesystems without altering the host environment or original project files:
```bash
make integration-cow        # Test OverlayFS copy-on-write, device mounts & cleanup
make integration-net        # Test Network namespace isolation & loopback IPC
make integration-security   # Test Capability dropping, NoNewPrivs & setuid protection
make integration            # Run all three integration suites sequentially
```

### 10. Which features remain unsupported?
- **User Namespaces (`CLONE_NEWUSER`):** Container runs with host UID 0 (root). While all capabilities are zeroed, user namespace ID mapping is not implemented.
- **Seccomp Syscall Filtering:** BPF syscall filters are not attached.
- **Virtual Ethernet & External Networking:** No `veth` pair bridging, NAT, port forwarding, or internet access (loopback-only).
- **Mandatory Access Control (MAC):** AppArmor and SELinux profiles are omitted.
- **OCI Specification:** Does not parse `config.json` bundles.

---

## Core Architecture & Execution Sequence

```mermaid
flowchart TD
    A[User Command: minic run ...] --> B[CLI Parsing & Validation]
    B --> C[Parent Manager]
    C --> D[Stage Rootfs to Linux-Native /tmp/minic-run-*]
    D --> E[Create Anonymous Sync Pipe]
    E --> F[Spawn Child with CLONE_NEWPID | CLONE_NEWUTS | CLONE_NEWNS | CLONE_NEWNET]

    F --> G[Child Blocks Reading from Sync Pipe]
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
    U --> V[Child: Resolve Target Executable Path via LookPath]
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
- **Privilege Hardening:** Strips all 5 capability sets and enables `no_new_privs` immediately before `execve`, ensuring the application cannot perform privileged syscalls even with UID 0.
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
| Signal forwarding            | Yes (Graceful escalation)     | Yes                          |
| Exit code preservation       | Yes (Exact + 128+N signals)   | Yes                          |
| CLI                          | Simplified (`run`, `child`)   | Full OCI runtime interface   |
| OCI specification            | No                            | Yes                          |
| Docker image handling        | No                            | No, handled by higher layers |
| User namespaces              | No (Host UID 0)               | Yes                          |
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
| Memory Limits (`memory.max`) | Yes | Yes (`test/integration_cow_test.sh`) | **VERIFIED** |
| CPU Limits (`cpu.max`) | Yes | Yes (`test/integration_cow_test.sh`) | **VERIFIED** |
| PID Limits (`pids.max`) | Yes | Yes (`test/integration_cow_test.sh`) | **VERIFIED** |
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
| User Namespaces (`CLONE_NEWUSER`) | — | — | **UNSUPPORTED** |
| Seccomp BPF Syscall Filtering | — | — | **UNSUPPORTED** |
| External veth / NAT Bridging | — | — | **UNSUPPORTED** |

---

## Project Structure
```text
minic/
├── .github/
│   └── workflows/
│       └── ci.yml               # GitHub Actions unprivileged CI workflow
├── cmd/
│   └── minic/
│       └── main.go              # CLI main entry point
├── internal/
│   ├── cgroup/                # cgroups v2 limits and lifecycle cleanup
│   ├── cli/                   # Argument parsing, usage, and exit codes
│   ├── container/             # Parent container manager & signal handling
│   ├── filesystem/            # Rootfs staging, OverlayFS mount, devices & pivot_root
│   ├── network/               # Network namespace & loopback ioctl configuration
│   ├── process/               # Child process initialization & namespace bootstrap
│   └── security/              # Capability dropping and no_new_privs hardening
├── test/
│   ├── integration_cow_test.sh  # Privileged OverlayFS copy-on-write integration test
│   ├── integration_net_test.sh  # Privileged Network isolation integration test
│   └── integration_sec_test.sh  # Privileged Security hardening & capability integration test
├── docs/
│   ├── demo-guide.md          # Step-by-step reproducible live demonstration guide
│   ├── viva-questions.md      # OS project viva examination questions and answers
│   ├── presentation-notes.md  # 5-minute presentation script
│   └── minic-architecture.md  # In-depth architectural design specifications
├── rootfs/                    # Bundled minimal BusyBox root filesystem
├── Makefile                   # Unprivileged developer automation & test targets
├── go.mod                     # Go module definition
├── AGENTS.md                  # Developer guidelines
└── README.md                  # This document
```

---

## Demonstration & Viva Resources
- **Live Demonstration:** See [docs/demo-guide.md](docs/demo-guide.md) for a complete, 7-part demonstration script with exact commands and expected outputs.
- **Viva Preparation:** See [docs/viva-questions.md](docs/viva-questions.md) for detailed technical answers to 13 common operating system examiner questions.
- **Presentation Script:** See [docs/presentation-notes.md](docs/presentation-notes.md) for a 5-minute presentation outline.
