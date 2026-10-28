# MiniC Architecture

This document describes the high-level architecture and execution flow for the MiniC runtime.

## 1. Overview
MiniC is a lightweight, educational Linux container runtime written in Go. Its goal is to demonstrate the fundamental OS-level mechanisms that power containers—Namespaces, Cgroups, and Filesystem isolation (`pivot_root`)—without the production-grade complexity found in reference runtimes like `runc`.

**Important**: MiniC is an independent Go implementation. The `runc` repository is used purely as a reference and is never mutated or imported by MiniC.

## 2. Core Execution Flow

The architecture operates strictly using a **Parent-Child process model**:

### Phase 1: Parent Setup
1. **CLI Parsing**: The user runs `minic run [options] <rootfs> <command>`.
2. **Configuration**: The CLI package parses the requested limits and target executable, passing them to the Container Manager.
3. **Cgroup Creation**: The Container Manager creates a unique cgroup v2 directory (e.g., `/sys/fs/cgroup/minic_<id>`) and applies CPU, memory, and PID limits.
4. **Child Spawning**: The Parent spawns a new instance of itself using `os/exec` (`/proc/self/exe child`), providing it with `unix.SysProcAttr{Cloneflags: unix.CLONE_NEWPID | unix.CLONE_NEWNS | unix.CLONE_NEWUTS}`. This places the new process in isolated namespaces from the moment it is born.
5. **Synchronization Setup**: The Parent provides an `os.Pipe` to the Child process for synchronization.

### Phase 2: Synchronization
1. **Child Blocking**: The newly spawned Child immediately blocks, waiting to read from the synchronization pipe.
2. **Parent Registration**: The Parent retrieves the Child's host PID and writes it to the `cgroup.procs` file inside the new cgroup. This guarantees that the Child cannot exceed resource limits before it begins execution.
3. **Parent Signal**: The Parent writes a byte to the synchronization pipe, waking up the Child.

### Phase 3: Child Isolation
1. **Mount Propagation**: The Child changes the root mount propagation to private to ensure its mount operations do not leak back to the host.
2. **Rootfs Bind Mount**: The Child bind-mounts the user-provided rootfs directory to itself.
3. **Procfs Setup**: The Child mounts a fresh `proc` filesystem at `<rootfs>/proc`.
4. **Pivot Root**: The Child calls `unix.PivotRoot`, swapping the host's root filesystem with the new rootfs, and then unmounts the old host root. (Filesystem safety: use controlled paths, always check for errors, and never destructively clean host paths).
5. **Hostname**: The Child sets the hostname for the isolated UTS namespace using `unix.Sethostname`.

### Phase 4: Execution
1. The Child uses `unix.Exec()` to overwrite its memory space with the requested user application (e.g., `/bin/sh`), running within the fully isolated environment.

### Phase 5: Cleanup
1. The Parent process, which has been waiting for the Child to exit, collects the Child's exit status.
2. The Parent removes the cgroup directory.
3. The Parent exits with the Child's exit code.

## 3. Structural Differences from Runc

- **Independent Project**: MiniC is built as a separate project (`minic/`), without compiling or modifying `runc`.
- **Modern Linux Syscalls**: MiniC favors `golang.org/x/sys/unix` for syscalls and constants over the deprecated `syscall` package.
- **Minimal Lifecycle State**: Runc uses complex state engine. MiniC relies on a minimal internal state: `Created`, `Starting`, `Running`, `Stopped`.
- **No CGO/`nsenter`**: Runc uses C-based `nsenter`. MiniC uses pure Go `SysProcAttr` with `os/exec.Cmd`.
- **Direct Cgroup File I/O**: MiniC strictly implements cgroups v2 using direct standard library file I/O operations.
- **Minimal Rootfs**: MiniC assumes a pre-prepared local rootfs (no Docker image management).
