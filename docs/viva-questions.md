# MiniC Viva Questions

**1. What is a container?**  
A container is a standard unit of software that packages up code and all its dependencies so the application runs quickly and reliably across different computing environments. At the OS level, it is simply an isolated and restricted Linux process.

**2. Is a container a virtual machine?**  
No. A VM provides hardware virtualization and runs an entire guest Operating System. A container is OS-level virtualization; it shares the host OS kernel and uses Namespaces and Cgroups for isolation.

**3. What is a PID namespace?**  
A PID (Process ID) namespace provides a process with an independent set of process IDs. A process inside a new PID namespace cannot see or interact with processes outside of it.

**4. Why does the container process become PID 1?**  
When a new PID namespace is created, the first process inside it is assigned PID 1. In Linux, PID 1 is the `init` process, responsible for managing the namespace and reaping orphaned child processes.

**5. What is a UTS namespace?**  
The UTS (UNIX Time-Sharing System) namespace isolates the hostname and NIS domain name, allowing the container to have a different hostname than the host machine.

**6. What is a mount namespace?**  
A mount namespace isolates the list of mount points seen by a process. This ensures that mounting or unmounting filesystems inside the container does not affect the host system.

**7. What are cgroups?**  
Control Groups (cgroups) are a Linux kernel feature that limits, accounts for, and isolates the resource usage (CPU, memory, disk I/O, network) of a collection of processes.

**8. What is cgroups v2?**  
Cgroups v2 is the modern, unified hierarchy version of cgroups. It simplifies resource management by ensuring a process can only belong to a single cgroup hierarchy, eliminating the inconsistencies of cgroups v1.

**9. What does `memory.max` do?**  
It defines the absolute hard limit of memory a cgroup (and its processes) can consume before the kernel's Out-Of-Memory (OOM) killer steps in.

**10. What does `cpu.max` do?**  
It defines CPU bandwidth limits using a quota and a period, dictating exactly how much CPU time the container can use over a specific timeframe (e.g., throttling to 50% CPU).

**11. What does `pids.max` do?**  
It places a cap on the maximum number of processes (PIDs) that can be created within the cgroup, preventing fork bombs and process exhaustion attacks.

**12. Why is synchronization needed?**  
The child process must be paused immediately after creation so the parent process has time to configure cgroups and apply resource limits *before* the child process executes the user application.

**13. Why does the parent need the host PID?**  
Cgroup management occurs on the host. The parent must use the child's real host PID to add it to the cgroup's `cgroup.procs` file, because the container's internal PID (which is 1) is meaningless to the host kernel.

**14. Why use `pivot_root()`?**  
`pivot_root()` swaps the mount point of the root filesystem (`/`). It effectively moves the host's root out of the way and establishes the container's isolated rootfs as the true root of the process.

**15. Why mount `/proc`?**  
The `/proc` virtual filesystem exposes kernel data (like active processes). Since the container is in a new PID namespace, it needs a freshly mounted `/proc` to correctly list its own isolated processes instead of the host's processes.

**16. What does `exec()` do?**  
The `execve` system call overwrites the current process's memory space and execution thread with a new program. MiniC uses it so the `init` process physically transforms into the requested application (like `/bin/sh`).

**17. What is rootfs?**  
A rootfs (Root Filesystem) is a directory containing all the binaries, libraries, and file hierarchies (like `/bin`, `/etc`, `/usr`) necessary to run an operating system environment.

**18. How is MiniC different from Docker?**  
Docker is a massive, multi-layered platform that manages images, networks, storage volumes, and registries. MiniC is a low-level runtime that simply takes an existing directory and executes a process inside it using kernel primitives.

**19. How is MiniC different from runc?**  
`runc` is the industry-standard, production-ready, OCI-compliant runtime that powers Docker. MiniC is a severely reduced, educational clone built to demonstrate the underlying OS mechanics without the production overhead.

**20. What are the project's limitations?**  
MiniC relies entirely on Linux APIs, missing advanced features like networking, capabilities, seccomp filtering, and overlayfs. It cannot be run natively on Windows/macOS and is not secure for production.
