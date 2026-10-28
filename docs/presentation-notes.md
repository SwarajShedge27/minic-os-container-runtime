# MiniC Presentation Notes

## 1. Problem Statement (1 min)
"Good [morning/afternoon]. Today I'll be presenting MiniC, a lightweight Linux Container Runtime built from scratch in Go. The modern software ecosystem relies heavily on containers—like Docker—to deploy applications reliably. But what exactly is a container? A container isn't a magical box or a Virtual Machine; it's simply an isolated Linux process. I built MiniC to peer under the hood of Docker and demonstrate exactly which Operating System primitives make this isolation possible."

## 2. MiniC Architecture & Namespaces (1.5 mins)
"To isolate a process, MiniC uses Linux Namespaces. When the parent MiniC process spawns the container process, it explicitly flags the kernel to create new Namespaces. 
- We use the **PID namespace** so the container cannot see the host's processes. 
- We use the **UTS namespace** so the container can have its own hostname. 
- We use the **Mount namespace** to isolate filesystem interactions. 
This is why our application thinks it's running alone as PID 1."

## 3. Resource Control with Cgroups (1 min)
"But isolation isn't enough; we need to control resources so a container doesn't crash the host. For this, MiniC uses **Linux cgroups v2**. The parent process grabs the child's Host PID and assigns it to a newly created `minic` control group. By writing directly to kernel files like `memory.max`, `cpu.max`, and `pids.max`, we strictly enforce how much memory, CPU, and how many processes our container is allowed to consume."

## 4. Rootfs and pivot_root (1.5 mins)
"To make the container feel like an entirely different operating system, we manipulate the filesystem. We provide MiniC with a `rootfs`—a simple folder containing basic OS binaries. Through a syscall called `pivot_root`, MiniC literally swaps the root of the filesystem so the container views this folder as the entire hard drive. We then unmount the host root, preventing any access to the host's files, and mount a fresh `/proc` filesystem so commands like `ps` work correctly."

## 5. Synchronization (1 min)
"One of the hardest architectural challenges was synchronization. We can't let the container run its application before the limits are applied. MiniC solves this using an Inter-Process Communication pipe. The child process is paused immediately upon creation, waiting for a 1-byte signal from the parent. The parent configures the cgroups, writes the limits, and only then pushes the byte through the pipe to release the child."

## 6. Live Demo (1 min)
*(If running on Linux/WSL)*
"Let's look at it in action. I'll launch MiniC with a shell: `sudo ./minic run --hostname minic-demo ./rootfs /bin/sh`.
As you can see, if I run `hostname`, it says `minic-demo`. If I run `echo $$`, my process ID is 1. If I run `ls /`, I only see the rootfs files, not the host files. The isolation is complete."

## 7. Limitations & Future Work (1 min)
"While MiniC successfully demonstrates core containerization, it is strictly educational. It lacks networking capabilities, security profiles like seccomp or AppArmor, and it doesn't parse standard Docker images. Future work could involve bringing it up to standard OCI (Open Container Initiative) compliance, allowing it to natively interface with Docker or Kubernetes. Thank you."
