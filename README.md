# MiniC — Lightweight Linux Container Runtime from Scratch

MiniC is a lightweight educational Linux container runtime built from scratch in Go that demonstrates process isolation, resource control, and filesystem isolation using Linux namespaces, cgroups v2, and rootfs/pivot_root.

## Overview
A Linux container is simply an isolated process (or set of processes) running on a host Linux machine, constrained by specific kernel features. MiniC is a from-scratch implementation designed for educational purposes to unveil exactly what goes on under the hood of container tools like Docker and `runc`. This project demonstrates the lowest-level Linux primitives—namespaces, cgroups, and filesystem isolation—without the production-grade complexity of standard industry tools. Note: MiniC is strictly an educational tool and is not production-ready.

## Core Architecture

```mermaid
flowchart TD
    A[User Command] --> B[CLI / Config]
    B --> C[Parent Manager]
    C --> D[Child Process]
    D --> E[PID Namespace]
    D --> F[UTS Namespace]
    D --> G[Mount Namespace]

    C --> H[cgroups v2]
    H --> I[CPU]
    H --> J[Memory]
    H --> K[PIDs]

    C --> L[Sync Pipe]

    D --> M[Rootfs Setup]
    M --> N[pivot_root]
    N --> O[/proc]
    O --> P[Hostname]
    P --> Q[exec()]
    Q --> R[Application]

    R --> C
    C --> S[Cleanup]
```

### Lifecycle:
1. **User Command**: The user invokes `minic run` with resource configurations and an application path.
2. **CLI / Config**: The CLI parses flags and builds the configuration for the runtime.
3. **Parent Manager**: Spawns the child container process and coordinates resource limits.
4. **Create Child & Namespaces**: The child process is created via `/proc/self/exe child`, directly inheriting new PID, UTS, and Mount namespaces.
5. **cgroups v2**: The Parent extracts the child's host PID, initializes a new cgroup, enforces resource limits, and attaches the child.
6. **Synchronization**: The Parent writes a byte to a pipe to signal the child (which was paused) to resume execution.
7. **Filesystem Setup**: The child sets up a private mount namespace and bind-mounts the rootfs.
8. **pivot_root()**: The child changes its root filesystem from the host to the supplied rootfs.
9. **/proc**: The `/proc` directory is mounted inside the new rootfs to provide process details to the containerized environment.
10. **Hostname & exec()**: The child sets its hostname and replaces itself with the requested application (e.g. `/bin/sh`).
11. **Parent Waits & Cleanup**: The Parent waits for the container process to exit, preserves the exact exit code, and purges the cgroups directory.

## Core Linux Concepts Demonstrated

### Namespaces
- **PID**: Process ID isolation. The container process becomes PID 1 and cannot see processes running on the host machine.
- **UTS**: Hostname isolation. The container can define its own hostname (`--hostname`) independently of the host.
- **Mount**: Filesystem and mount isolation. The container can mount/unmount paths without affecting the host machine.

### cgroups v2
Control Groups (cgroups) govern the resource utilization of a process. MiniC implements:
- `memory.max`: Memory limits for the container. Accepts suffixes `K`, `M`, `G`, raw bytes, or `max` for unlimited. Values must be strictly positive integers.
- `cpu.max`: CPU throttling and utilization caps. Expressed as percentage (e.g., `50` for 50% of 1 core, `150` for 1.5 cores).
- `pids.max`: Limits on the number of processes that can be spawned inside the container. Must be a positive integer.

### Signal Handling & Exit Status
- **Signal Forwarding**: The parent intercepts `SIGINT` (Ctrl+C) and `SIGTERM`, forwards them to the container process, and waits for termination with a 5-second grace period before escalating to `SIGKILL`.
- **Exit Status**: Conventional exit codes are preserved. Processes terminated by a signal `N` return the standard shell status `128 + N` (e.g., 137 for SIGKILL/OOM, 130 for SIGINT, 143 for SIGTERM).
- **Cgroup Cleanup**: Upon process exit, any lingering processes in `cgroup.procs` are terminated and the owned cgroup directory is safely removed with bounded retries.

### Filesystem Isolation
- **rootfs**: An independent filesystem layout (like Alpine Linux root files) that the container views as its entire hard drive.
- **pivot_root**: A Linux syscall that shifts the process's root mount to the supplied rootfs and detaches the old host root.
- **/proc**: The virtual filesystem exposing kernel process metrics. It must be explicitly remounted within the container's PID namespace so commands like `ps` function correctly.
- **exec()**: The `execve` syscall replaces the current child initialization process entirely with the final containerized application.

## Comparison with runc

| Feature                      | MiniC       | runc                         |
| ---------------------------- | ----------- | ---------------------------- |
| Linux namespaces             | Yes         | Yes                          |
| cgroups v2                   | Yes         | Yes                          |
| rootfs isolation             | Yes         | Yes                          |
| pivot_root                   | Yes         | Yes                          |
| /proc                        | Yes         | Yes                          |
| CLI                          | Simplified  | Full runtime interface       |
| OCI specification            | No          | Yes                          |
| Docker image handling        | No          | No, handled by higher layers |
| Full lifecycle management    | Limited     | Extensive                    |
| Production security features | Limited     | Extensive                    |
| Goal                         | Educational | Production container runtime |

## Project Structure
```text
minic/
├── cmd/
│   └── minic/
│       └── main.go         # Application entry point
├── internal/
│   ├── cli/              # Command-line parsing and config routing
│   ├── container/        # Parent process management and lifecycle execution
│   ├── process/          # Child process initialization and execution
│   ├── cgroup/           # cgroups v2 resource limits configuration
│   └── filesystem/       # Mount namespaces and pivot_root operations
├── docs/                 # Architectural plans and presentation support
├── go.mod                # Go module definitions
├── AGENTS.md             # Developer guidelines
└── README.md             # This document
```

## Testing

Unit tests run unprivileged without requiring `sudo` or modifying the host cgroup hierarchy:

```bash
# Run all unit tests
go test -v ./...

# Run static analysis
go vet ./...
```

## Demo

**NOTE:** The features are completely *Implemented* structurally, but they are *Not yet verified* on the development workspace due to the reliance on Linux-only kernel features. Execution and Verification require a proper Linux environment or WSL.

### Basic execution
```bash
sudo ./minic run ./rootfs /bin/echo hello
```
### Interactive shell
```bash
sudo ./minic run ./rootfs /bin/sh
```
### Hostname isolation
```bash
sudo ./minic run --hostname minic-demo ./rootfs /bin/sh
```
### Resource limits
```bash
sudo ./minic run \
  --memory 256M \
  --cpu 50 \
  --pids 20 \
  ./rootfs /bin/sh
```
### Debug mode
```bash
sudo ./minic run \
  --debug \
  --memory 256M \
  --cpu 50 \
  --pids 20 \
  --hostname minic-demo \
  ./rootfs /bin/sh
```

## Demo Verification Checklist

| Test                   | What it demonstrates  |
| ---------------------- | --------------------- |
| `echo hello`           | Basic execution       |
| `echo $$`              | PID namespace         |
| `hostname`             | UTS namespace         |
| `ls /`                 | rootfs isolation      |
| `ls /proc`             | proc filesystem       |
| resource-limit run     | cgroups               |
| `exit 42` + shell `$?` | exit-code propagation |
| `--debug`              | runtime lifecycle     |

## Limitations
- **Linux-only**: Relies heavily on Linux POSIX and kernel APIs (Namespaces, cgroups v2). It cannot be compiled or tested natively on Windows or macOS.
- **No Networking**: Containers lack a dedicated network namespace.
- **Not OCI-Compliant**: Does not parse a `config.json` standard OCI bundle.
- **Missing Docker Features**: Does not download images, support `overlayfs`, or handle image layers.
- **Security Hardening**: Missing AppArmor, SELinux, Capabilities, and Seccomp profiles.
- **Rootless Mode**: Not supported. 
- **Validation**: Current implementations were architected on Windows, thus verification tests require transferring the source to a Linux/WSL environment.

## Future Work
- OCI `config.json` support
- Addition of network namespaces
- Implementation of Linux capabilities and seccomp filtering
- Rootless containers
- Richer lifecycle commands (start, stop, delete, exec)
- Implementation of a rigorous integration test suite
