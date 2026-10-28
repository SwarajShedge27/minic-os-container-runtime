# MiniC — College Presentation Notes (5-7 Minutes)

## 1. Problem Statement & Motivation (1 min)
"Good [morning/afternoon]. Today I am presenting **MiniC**, a lightweight Linux container runtime written from scratch in Go.
The modern cloud ecosystem relies universally on container engines like Docker and Kubernetes. But what actually is a container under the hood?
A container is not a virtual machine and has no guest kernel. It is simply an ordinary Linux process wrapped in kernel isolation primitives.
I engineered MiniC to unveil the lowest-level operating system mechanisms that make containers work: Linux Namespaces, Control Groups v2, OverlayFS union filesystems, `pivot_root`, and Linux Capabilities."

## 2. Linux Namespaces & Process Isolation (1.5 mins)
"To isolate what the containerized application can *see*, MiniC configures four core Linux namespaces during process cloning:
- **PID Namespace (`CLONE_NEWPID`):** Isolates the process hierarchy. Inside the container, the spawned process becomes PID 1, completely blinded to host processes.
- **UTS Namespace (`CLONE_NEWUTS`):** Isolates the system hostname and NIS domain.
- **Mount Namespace (`CLONE_NEWNS`):** Isolates filesystem mount tables so internal mounts never leak to the host.
- **Network Namespace (`CLONE_NEWNET`):** Isolates the entire network stack. All host interfaces like `eth0` are hidden. MiniC brings up the loopback interface (`lo`) with IP `127.0.0.1` using socket ioctl syscalls, allowing secure local IPC without exposing external networks."

## 3. Resource Limiting with cgroups v2 (1 min)
"Isolation alone is insufficient; we must prevent a container from starving host resources. MiniC leverages **Linux cgroups v2** (unified hierarchy).
By writing directly to kernel control files:
- `memory.max` enforces hard physical memory limits.
- `cpu.max` enforces CPU bandwidth quotas over a 100ms period (e.g., throttling to 50% of a core).
- `pids.max` caps maximum concurrent processes to neutralize fork-bomb attacks.
Crucially, MiniC uses **pipe synchronization**: the child pauses upon birth, allowing the parent to bind its host PID into `cgroup.procs` *before* the application ever executes."

## 4. Filesystem Isolation: OverlayFS & pivot_root (1.5 mins)
"To provide a distinct filesystem, MiniC uses a bundled BusyBox rootfs paired with **OverlayFS copy-on-write**:
- The base image acts as an immutable, read-only `lowerdir`.
- All writes, modifications, and deletions occur in an ephemeral `upperdir`.
- Essential character devices (`/dev/null`, `/dev/zero`, `/dev/random`, `/dev/urandom`) are bind-mounted into `<merged>/dev`.
- Through `pivot_root`, MiniC swaps the host root mount with the merged OverlayFS and detaches the old root.
- A fresh `/proc` filesystem is mounted to reflect containerized PID 1 metrics.
When the container terminates, the temporary staging directory is unmounted and erased, guaranteeing that the original rootfs is never modified."

## 5. Privilege Hardening: Capabilities & no_new_privs (1 min)
"In Linux, root privilege is governed by discrete **capabilities**, not just UID 0.
Before launching the user command, MiniC permanently pins its thread (`runtime.LockOSThread()`) and drops all capabilities:
- The capability bounding set is cleared (`PR_CAPBSET_DROP`).
- Ambient capabilities are reset (`PR_CAP_AMBIENT_CLEAR_ALL`).
- `PR_SET_NO_NEW_PRIVS` is asserted, preventing setuid binaries from escalating privileges.
- Effective, Permitted, and Inheritable sets are zeroed via `unix.Capset`.
As a result, even though the process runs with UID 0, operations like `sethostname`, raw socket packet creation (`ping`), and filesystem mounts are blocked with `EPERM`."

## 6. Live Demonstration Highlights (1 min)
"In our live demo, we prove:
1. **PID & UTS Isolation:** PID inside is 1; hostname is isolated.
2. **Network Isolation:** Only `lo` exists with `127.0.0.1`; TCP roundtrip works over local loopback.
3. **Copy-on-Write:** Files created inside the container disappear upon exit; base image stays untouched.
4. **Security Hardening:** `/proc/self/status` shows all zeroes for capabilities and `NoNewPrivs: 1`; privileged syscalls are rejected.
5. **Automatic Cleanup:** Mounts, staging directories, and cgroup nodes are 100% purged."

## 7. Educational Scope & Limitations (0.5 min)
"MiniC is an educational container runtime designed to demonstrate kernel primitives.
It intentionally omits production-grade features like User Namespaces (`CLONE_NEWUSER`), Seccomp BPF syscall filtering, AppArmor/SELinux profiles, and OCI bundle parsing.
Thank you, and I look forward to your questions."
