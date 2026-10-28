# MiniC — Operating Systems Project Viva Questions & Answers

This document provides concise, technically rigorous answers to viva and examination questions based on the actual MiniC implementation.

---

### 1. What is the fundamental difference between a container and a virtual machine?
- **Virtual Machine (VM):** Relies on a hypervisor (Type 1 or Type 2) to emulate virtual hardware. Each VM runs a full guest operating system with its own kernel, device drivers, and memory overhead.
- **Container:** An isolated user-space process (or process tree) running directly on the host Linux kernel. It leverages kernel primitives—specifically **namespaces** for boundary isolation and **cgroups** for resource throttling—without running a separate operating system kernel.

---

### 2. What is the difference between Linux Namespaces and Control Groups (cgroups)?
- **Namespaces isolate what a process can SEE:** They virtualize system resources (PIDs, network devices, mount points, hostnames, IPC) so a process believes it has its own dedicated operating system instance.
- **Control Groups (cgroups) isolate what a process can USE:** They meter, account for, and constrain resource consumption (CPU time, physical memory, maximum process count, I/O bandwidth).

---

### 3. Which namespaces does MiniC configure, and what is the role of each?
MiniC combines four clone flags via `unix.SysProcAttr{Cloneflags: ...}`:
1. `CLONE_NEWPID` (**PID Namespace**): Gives the child process an independent PID space. The container's initial process becomes PID 1 and cannot see or signal host processes.
2. `CLONE_NEWUTS` (**UTS Namespace**): Isolates the hostname and NIS domain, allowing `unix.Sethostname` to set a container-specific hostname without affecting the host.
3. `CLONE_NEWNS` (**Mount Namespace**): Isolates mount points. Filesystems mounted or unmounted inside the container do not pollute the host mount hierarchy.
4. `CLONE_NEWNET` (**Network Namespace**): Isolates network stack interfaces, routing tables, and socket tables. Host interfaces (`eth0`, `wlan0`) are completely hidden.

---

### 4. Why does MiniC require parent-child synchronization via an anonymous pipe?
When `CLONE_NEWPID` is used, the child process does not know its host PID—inside its namespace, it is PID 1. However, cgroups v2 operates on the host kernel filesystem (`/sys/fs/cgroup/minic-<pid>/cgroup.procs`), which requires the **child's host PID**.
- If the child ran immediately, it could execute user commands and consume unmetered resources before the parent finished configuring cgroup limits.
- **Solution:** The child blocks on reading 1 byte from an anonymous pipe file descriptor (`FD 3`). The parent grabs the child's host PID, initializes the cgroup, writes `memory.max`, `cpu.max`, `pids.max`, registers the child's host PID into `cgroup.procs`, and only then writes the 1-byte unlock token to the pipe.

---

### 5. How does `pivot_root` differ from `chroot` or changing the working directory?
- **`chdir` / changing directory:** Only changes the process's current working directory pointer (`PWD`). System calls taking absolute paths (`/etc/passwd`) or relative traversal (`../../`) still access the original root filesystem.
- **`chroot`:** Modifies the apparent root directory for path resolution, but does not move the mount point. A process holding an open file descriptor outside the chroot or possessing root permissions can easily escape (`fchdir` breakout).
- **`pivot_root`:** Swaps the actual mount table entry of the root filesystem. It makes the new directory (in MiniC, the merged OverlayFS) the root mount of the mount namespace, moves the old host root to a temporary sub-directory (`/.minic_oldroot`), and permits unmounting the old root via `umount2(..., MNT_DETACH)`. The old host root is completely detached from the process's mount table.

---

### 6. How does OverlayFS implement copy-on-write (COW)?
OverlayFS is a union filesystem service combining multiple directory layers:
- **`lowerdir` (Read-Only):** The base container rootfs image. Files read from here are never modified.
- **`upperdir` (Read-Write):** An ephemeral directory where all new files, modifications, and deletions are recorded.
- **`workdir` (Internal):** A scratch directory used by OverlayFS for atomic operations (such as file copy-up before mutation).
- **`merged` (Unified Mount Point):** The mount target presented to the container.
When a file in `lowerdir` is opened for writing, OverlayFS transparently copies the file to `upperdir` (copy-up) before applying writes. Deleted files create special "whiteout" character devices (`0/0`) in `upperdir`. The original base image remains byte-for-byte untouched.

---

### 7. Why does WSL 2 require rootfs staging to `/tmp` before mounting OverlayFS?
In WSL 2, the Windows workspace resides under `/mnt/c` via 9P/drvfs. The 9P/drvfs driver does not support Linux POSIX extended attributes (`trusted.overlay.*`), mandatory file modes, or character device creation required by OverlayFS. MiniC detects this constraint and automatically copies the rootfs to an ephemeral directory (`/tmp/minic-run-*`) located on the Linux-native `ext4`/`tmpfs` filesystem before executing the OverlayFS mount.

---

### 8. What resource limits do cgroups v2 control in MiniC?
1. **`memory.max`:** Enforces the hard upper memory limit in bytes. Suffixes `K`, `M`, `G` are converted to bytes. If exceeded, the kernel invokes the OOM killer on container processes.
2. **`cpu.max`:** Configures CPU quota over a $100000\,\mu\text{s}$ (100 ms) period. For example, `--cpu 50` writes `50000 100000` (50% of 1 CPU core), throttling CPU bandwidth.
3. **`pids.max`:** Caps the maximum number of simultaneous tasks (processes and threads) inside the cgroup, preventing fork bombs.

---

### 9. Why does loopback networking work if external networking is disconnected?
When `CLONE_NEWNET` creates a network namespace, it initializes a pristine, empty network stack with a single down interface: `lo`.
MiniC uses socket `ioctl` calls (`SIOCGIFFLAGS` and `SIOCSIFFLAGS`) with `IFF_UP | IFF_RUNNING` to activate the loopback interface with IPv4 address `127.0.0.1`.
Because loopback routes packets entirely through internal kernel memory buffers without touching physical NICs, default gateways, or ARP tables, containerized processes can perform local IPC and TCP/UDP communication safely while remaining completely disconnected from the external network and host interfaces.

---

### 10. What is the difference between UID 0 and Linux capabilities?
Historically in UNIX, privilege was binary: UID 0 was omnipotent (superuser), while non-zero UIDs were unprivileged.
Modern Linux divides superuser privileges into discrete units called **capabilities** (`CAP_SYS_ADMIN`, `CAP_NET_RAW`, `CAP_NET_BIND_SERVICE`, `CAP_SETUID`, etc.).
- Even if a process has `UID == 0` (root), if its capability sets are stripped, it **cannot** perform privileged operations.
- MiniC demonstrates this principle: the container runs with `UID 0`, but because `CapEff`, `CapPrm`, `CapInh`, `CapBnd`, and `CapAmb` are zeroed, operations like `sethostname`, raw packet creation, and filesystem mounts are denied by the kernel with `EPERM`.

---

### 11. What does `PR_SET_NO_NEW_PRIVS` guarantee, and why is it needed?
Enabling `no_new_privs` via `unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, ...)` guarantees that the calling thread and all its descendants can **never** gain privileges across `execve`.
- Specifically, execution of binaries with setuid (`chmod 4755`) or setgid bits, or binaries with filesystem capabilities (`setcap`), will **not** grant elevated credentials.
- In MiniC, this prevents an attacker from executing a setuid binary within the container rootfs to regain root capabilities.

---

### 12. Why is MiniC an educational runtime rather than a production sandbox?
MiniC demonstrates the fundamental OS mechanisms used by Docker and runc, but lacks several production-grade security boundaries:
1. **No User Namespaces (`CLONE_NEWUSER`):** The container process runs as real host UID 0. While capabilities are dropped, any kernel API checking raw UID rather than capabilities (or any unpatched kernel vulnerability) could be leveraged.
2. **Unmasked Host Virtual Filesystems:** Sensitive `/proc` and `/sys` entries (such as `/proc/sys`, `/proc/sysrq-trigger`, `/sys/firmware`) are not mounted read-only or masked.
3. **No Seccomp Syscall Filtering:** Syscalls like `bpf()`, `ptrace()`, or `keyctl()` are not blocked by a Berkeley Packet Filter (BPF).
4. **No Mandatory Access Control (MAC):** AppArmor or SELinux profiles are not bound to the container.

---

### 13. How does MiniC ensure clean resource recovery when a container exits or receives a signal?
MiniC uses Go's LIFO `defer` mechanism:
1. Parent intercepts `SIGINT` (Ctrl+C) and `SIGTERM`, forwarding them to the container process. If the container does not exit within 5 seconds, it escalates to `SIGKILL`.
2. When the wait concludes, `cg.Cleanup()` runs first: it kills lingering tasks in `cgroup.kill` or `cgroup.procs`, and removes the `/sys/fs/cgroup/minic-<pid>` directory with bounded retries.
3. Next, `staging.Cleanup()` runs: it recursively unlinks the temporary staging directory `/tmp/minic-run-*`, automatically releasing the OverlayFS scratch layers.
4. Finally, the parent exits with the container's exact exit code or signal offset (`128 + signal`).
