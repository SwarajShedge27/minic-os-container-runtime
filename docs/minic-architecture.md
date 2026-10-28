# MiniC Architecture

This document describes the high-level architecture and execution flow for the MiniC runtime.

## 1. Overview
MiniC is a lightweight, educational Linux container runtime written in Go. Its goal is to demonstrate the fundamental OS-level mechanisms that power containers—Namespaces (PID, Mount, UTS, Network), Cgroups v2, and Filesystem isolation (`pivot_root`, OverlayFS)—without the production-grade complexity found in reference runtimes like `runc`.

**Important**: MiniC is an independent Go implementation. The `runc` repository is used purely as a reference and is never mutated or imported by MiniC.

## 2. Core Execution Flow

The architecture operates strictly using a **Parent-Child process model**:

### Phase 1: Parent Setup
1. **CLI Parsing**: The user runs `minic run [options] <rootfs> <command>`.
2. **Configuration**: The CLI package parses the requested limits and target executable, passing them to the Container Manager.
3. **Rootfs Staging**: Because the workspace may reside on Windows-backed filesystems (e.g. `/mnt/c` under WSL 2) where OverlayFS cannot mount directly, the Parent stages a clean copy of the rootfs into a unique, restricted directory on a Linux-native filesystem (`/tmp/minic-run-<id>/lower`). It prepares `upper`, `work`, and `merged` mount directories.
4. **Cgroup Creation**: The Container Manager creates a unique cgroup v2 directory (e.g., `/sys/fs/cgroup/minic-<pid>`) and applies CPU, memory, and PID limits.
5. **Child Spawning**: The Parent spawns a new instance of itself using `os/exec` (`/proc/self/exe child`), providing it with `unix.SysProcAttr{Cloneflags: unix.CLONE_NEWPID | unix.CLONE_NEWNS | unix.CLONE_NEWUTS | unix.CLONE_NEWNET}`. This places the new process in isolated PID, Mount, UTS, and Network namespaces from the moment it is born.
6. **Synchronization Setup**: The Parent provides an `os.Pipe` to the Child process for synchronization.

### Phase 2: Synchronization
1. **Child Blocking**: The newly spawned Child immediately blocks, waiting to read from the synchronization pipe.
2. **Parent Registration**: The Parent retrieves the Child's host PID and writes it to the `cgroup.procs` file inside the new cgroup. This guarantees that the Child cannot exceed resource limits before it begins execution.
3. **Parent Signal**: The Parent writes a byte to the synchronization pipe, waking up the Child.

### Phase 3: Child Isolation & Filesystem
1. **Mount Propagation**: The Child changes the root mount propagation to private to ensure its mount operations do not leak back to the host.
2. **OverlayFS Mount**: The Child mounts a disposable OverlayFS onto the merged directory (`lowerdir=lower,upperdir=upper,workdir=work`), providing copy-on-write isolation. The caller's source rootfs is completely untouched.
3. **Device Provisioning**: The Child bind-mounts minimal safe device nodes (`/dev/null`, `/dev/zero`, `/dev/random`, `/dev/urandom`) into the container's `<merged>/dev`.
4. **Pivot Root**: The Child calls `unix.PivotRoot` swapping into the merged OverlayFS root, and detaches the old host root.
5. **Procfs Setup**: The Child mounts a fresh `proc` filesystem at `/proc`.
6. **Loopback Network Setup**: The Child configures its isolated network loopback interface (`lo`) to UP and RUNNING state using socket ioctls (`SIOCGIFFLAGS`/`SIOCSIFFLAGS`). Host interfaces (`eth0`, `wlan0`) are completely hidden.
7. **Hostname**: The Child sets the hostname for the isolated UTS namespace using `unix.Sethostname`.
8. **Binary Resolution**: The Child resolves the application binary path within the newly mounted rootfs using `exec.LookPath`.

### Phase 4: Privilege Hardening & Execution
1. **Thread Pinning (`runtime.LockOSThread`)**: Linux capabilities and `no_new_privs` are per-thread kernel attributes. The child goroutine is permanently pinned to its operating system thread to guarantee that all privilege changes apply directly to the thread performing the `execve` syscall.
2. **Execution Ordering**: Privilege reduction occurs strictly at the final step before `unix.Exec()`, after all privileged operations (mounting OverlayFS, bind mounting devices, `pivot_root`, mounting `/proc`, configuring network loopback ioctls, and `sethostname`) have completed.
3. **Dynamic Capability Discovery**: MiniC queries `/proc/sys/kernel/cap_last_cap` (with fallback to iterative `unix.PR_CAPBSET_READ` probing) to determine the kernel's supported capability range without hardcoding arbitrary bounds.
4. **Capability Bounding Set Dropping (`PR_CAPBSET_DROP`)**: The Child iterates from capability 0 to `lastCap` and drops each capability from the bounding set using `unix.Prctl(unix.PR_CAPBSET_DROP, ...)`. This is executed first while `CAP_SETPCAP` is still retained in the permitted set.
5. **Ambient Capabilities Reset (`PR_CAP_AMBIENT_CLEAR_ALL`)**: The Child clears all ambient capabilities via `unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, ...)`.
6. **Setting `no_new_privs` (`PR_SET_NO_NEW_PRIVS`)**: The Child enables the `no_new_privs` bit via `unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, ...)`, preventing child processes or setuid/setgid binaries from acquiring new capabilities or elevating privileges across `execve`.
7. **Clearing Effective, Permitted, and Inheritable Sets (`unix.Capset`)**: Using 64-bit `_LINUX_CAPABILITY_VERSION_3`, the Child zeros out all capability bitmasks.
8. **Command Execution**: The Child calls `unix.Exec()` to overwrite its memory image with the requested containerized process.

### Phase 5: Cleanup
1. The Parent process, which has been waiting for the Child to exit (forwarding SIGINT/SIGTERM if caught), collects the Child's exit status.
2. The Parent terminates any lingering cgroup processes and removes the cgroup directory.
3. The Parent removes the temporary OverlayFS staging directory (`/tmp/minic-run-<id>`), discarding all container writes.
4. The Parent exits with the Child's exit code.

## 3. Security Model and Limitations

MiniC's Phase 4 security model introduces real privilege hardening, but it is important to understand its boundaries:
- **No Capabilities (`CapEff: 0`, `CapPrm: 0`, `CapInh: 0`, `CapBnd: 0`, `CapAmb: 0`)**: The containerized process cannot perform privileged operations such as mounting filesystems, modifying network interfaces, creating raw ICMP sockets, or changing the system clock, even though its effective UID is 0.
- **`no_new_privs` Enforcement**: Execution of setuid/setgid binaries cannot grant capabilities or elevate privileges.
- **Host UID 0 vs. User Namespaces**: MiniC does not currently implement Linux User Namespaces (`CLONE_NEWUSER`). The container process still runs with real UID 0 (root). While capabilities are stripped, any host resources or kernel interfaces that check raw UID 0 rather than capabilities (or kernel vulnerabilities) could potentially be accessed. MiniC is designed as an educational tool, not an impenetrable production multi-tenant sandbox.
- **Unmasked Virtual Filesystems**: Special virtual filesystem paths such as `/proc/sys`, `/proc/sysrq-trigger`, and `/sys` are not masked or mounted read-only, unlike production container runtimes.

## 4. Structural Differences from Runc

- **Independent Project**: MiniC is built as a separate project (`minic/`), without compiling or modifying `runc`.
- **Modern Linux Syscalls**: MiniC favors `golang.org/x/sys/unix` for syscalls and constants over the deprecated `syscall` package.
- **Minimal Lifecycle State**: Runc uses complex state engine. MiniC relies on a minimal internal state: `Created`, `Starting`, `Running`, `Stopped`.
- **No CGO/`nsenter`**: Runc uses C-based `nsenter`. MiniC uses pure Go `SysProcAttr` with `os/exec.Cmd`.
- **Direct Cgroup File I/O**: MiniC strictly implements cgroups v2 using direct standard library file I/O operations.
- **Minimal Rootfs**: MiniC assumes a pre-prepared local rootfs (no Docker image management).
