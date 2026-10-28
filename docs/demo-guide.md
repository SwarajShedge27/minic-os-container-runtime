# MiniC — College Project Demonstration Guide

This guide provides a reproducible, step-by-step demonstration script for college project presentations, examiner vivas, and laboratory evaluations.

All demonstrations run safely on **Linux** or **WSL 2 (Ubuntu)** with root privileges (`sudo`).

---

## Prerequisites & Pre-Demo Setup

1. **Verify Project Root and Build the Binary:**
   ```bash
   cd minic
   make build
   ```
   *Expected Output:*
   ```text
   Building MiniC binary to bin/minic...
   Build complete: bin/minic
   ```

2. **Verify Bundled Rootfs:**
   ```bash
   ls -la rootfs/bin/busybox
   ```
   *Verification:* Ensure the minimal BusyBox rootfs is present with executable permissions.

---

## Demonstration Script

### Demo 1: Basic Command Execution
**Objective:** Prove that MiniC can stage a rootfs, mount an OverlayFS, pivot into it, and execute an application.

```bash
sudo ./bin/minic run ./rootfs /bin/echo "Hello from MiniC!"
```

- **Expected Output:**
  ```text
  Hello from MiniC!
  ```
- **OS Concepts Demonstrated:**
  - Child process creation with namespace flags.
  - Parent-child pipe synchronization.
  - Staging to Linux-native ext4 mount.
  - OverlayFS mount & `pivot_root`.
  - `unix.Exec()` application handover.

---

### Demo 2: Linux Namespace Isolation
**Objective:** Prove that the container possesses isolated PID, UTS, and Network namespaces.

1. **PID Namespace Isolation:**
   ```bash
   # Host PID
   echo "Host Shell PID: $$"

   # Container PID (via interactive shell or piped command)
   sudo ./bin/minic run ./rootfs /bin/sh -c 'echo "Container Internal PID: $$"'
   ```
   *Alternative interactive demonstration:*
   ```bash
   sudo ./bin/minic run ./rootfs /bin/sh
   # Inside container shell:
   echo $$
   exit
   ```
   - **Expected Output:**
     - Host PID: a large integer (e.g., `12450`).
     - Container PID: strictly `1`.
   - **OS Concept:** `CLONE_NEWPID` isolates the process table. Inside the container, the spawned process is PID 1.

2. **UTS Namespace (Hostname) Isolation:**
   ```bash
   echo "Host Hostname: $(hostname)"
   sudo ./bin/minic run --hostname minic-isolated ./rootfs /bin/sh -c 'echo "Container Hostname: $(hostname)"'
   ```
   - **Expected Output:**
     - Host: host machine name (e.g. `DESKTOP-XYZ`).
     - Container: `minic-isolated`.
   - **OS Concept:** `CLONE_NEWUTS` enables independent hostname configuration without modifying the host system.

3. **Network Namespace Isolation:**
   ```bash
   echo "Host NetNS: $(readlink /proc/self/ns/net)"
   sudo ./bin/minic run ./rootfs /bin/sh -c 'echo "Container NetNS: $(readlink /proc/self/ns/net)"'
   ```
   - **Expected Output:** Distinct inode numbers (e.g. `net:[4026531833]` on host vs `net:[4026532228]` in container).
   - **OS Concept:** `CLONE_NEWNET` provides a dedicated network stack.

---

### Demo 3: Resource Management via cgroups v2
**Objective:** Demonstrate that CPU, Memory, and PID caps are enforced via the Linux cgroups v2 unified hierarchy.

1. **Memory, CPU, and PID Limits with Diagnostic Mode:**
   ```bash
   sudo ./bin/minic run --debug --memory 64M --cpu 50 --pids 20 ./rootfs /bin/echo "Resource limits applied"
   ```
   - **Expected Output:**
     ```text
     [MiniC] Starting container
     [MiniC] Staging rootfs into temporary OverlayFS directory
     [MiniC] Child host PID: ...
     [MiniC] Configuring cgroup
     [MiniC] Memory limit: 64M
     [MiniC] CPU limit: 50%
     [MiniC] PID limit: 20
     [MiniC] Added process to cgroup
     [MiniC] Releasing child
     ...
     Resource limits applied
     [MiniC] Cleaning up cgroup
     [MiniC] Cleaning up temporary filesystem staging
     [MiniC] Container stopped
     ```

2. **Safe, Bounded Process Throttling Demonstration:**
   ```bash
   sudo ./bin/minic run --pids 5 ./rootfs /bin/sh -c '
       echo "Spawning background tasks within pids.max=5 limit..."
       for i in 1 2 3 4; do
           /bin/busybox sleep 0.2 &
       done
       echo "Successfully created bounded process set."
       wait
   '
   ```
   - **OS Concept:** Writing to `pids.max`, `memory.max`, and `cpu.max` in `/sys/fs/cgroup/minic-<pid>` bounds container resource consumption.

---

### Demo 4: Filesystem Copy-on-Write (OverlayFS)
**Objective:** Prove that container writes and deletions are recorded in an ephemeral `upperdir` and discarded on exit, leaving the underlying rootfs 100% pristine.

```bash
# 1. Prepare disposable canary file in a scratch directory
SCRATCH_DIR=$(mktemp -d /tmp/minic-demo-cow.XXXXXX)
cp -a ./rootfs "$SCRATCH_DIR/rootfs"
echo "PRISTINE_BASE_DATA" > "$SCRATCH_DIR/rootfs/canary.txt"

echo "Before container run:"
cat "$SCRATCH_DIR/rootfs/canary.txt"

# 2. Modify and create files inside the container
sudo ./bin/minic run "$SCRATCH_DIR/rootfs" /bin/sh -c '
    echo "Container writing to canary file..."
    echo "CONTAINER_MUTATED_DATA" >> /canary.txt
    echo "CONTAINER_EPHEMERAL_FILE" > /new_file.txt
    echo "Inside container canary content:"
    cat /canary.txt
'

# 3. Inspect canary file on host
echo "After container exit (on host):"
cat "$SCRATCH_DIR/rootfs/canary.txt"

if [ -f "$SCRATCH_DIR/rootfs/new_file.txt" ]; then
    echo "ERROR: File leaked to host!"
else
    echo "PASS: New file never touched host rootfs."
fi

# Clean up demonstration scratch data
rm -rf "$SCRATCH_DIR"
```

- **Expected Output:**
  - Inside container: shows `CONTAINER_MUTATED_DATA`.
  - On host after exit: `canary.txt` remains strictly `PRISTINE_BASE_DATA`. `new_file.txt` does not exist.
- **OS Concept:** OverlayFS unions a read-only `lowerdir` with a disposable `upperdir`. All mutations are isolated and discarded at teardown.

---

### Demo 5: Network Namespace & Loopback Isolation
**Objective:** Show that the host network interfaces (`eth0`, `wlan0`) are completely hidden, while local loopback (`lo`) is configured and functional for inter-process communication.

1. **Verify Interface Isolation:**
   ```bash
   sudo ./bin/minic run ./rootfs /bin/busybox ifconfig
   ```
   - **Expected Output:** Only `lo` is visible, configured with `127.0.0.1` and flags `UP LOOPBACK RUNNING`. No host interfaces appear.

2. **Unprivileged Local IPC Test:**
   ```bash
   sudo ./bin/minic run ./rootfs /bin/sh -c '
       /bin/busybox nc -l -p 19090 -e /bin/busybox echo "loopback-ipc-success" &
       /bin/busybox sleep 0.2
       /bin/busybox nc -w 2 127.0.0.1 19090
   '
   ```
   - **Expected Output:**
     ```text
     loopback-ipc-success
     ```
- **OS Concept:** Socket `ioctl` syscalls (`SIOCGIFFLAGS`/`SIOCSIFFLAGS`) bring up `lo` within the child's `CLONE_NEWNET` stack. Containers have isolated localhost IPC without exposing host networks or requiring external internet access.

---

### Demo 6: Linux Capability Dropping & `no_new_privs`
**Objective:** Demonstrate that the container process runs with all Linux capabilities stripped to zero and `no_new_privs` enabled, rendering privileged syscalls impossible even though UID is 0.

1. **Inspect `/proc/self/status`:**
   ```bash
   sudo ./bin/minic run ./rootfs /bin/sh -c '/bin/cat /proc/self/status | /bin/busybox grep -E "Cap|NoNewPrivs"'
   ```
   - **Expected Output:**
     ```text
     CapInh:	0000000000000000
     CapPrm:	0000000000000000
     CapEff:	0000000000000000
     CapBnd:	0000000000000000
     CapAmb:	0000000000000000
     NoNewPrivs:	1
     ```

2. **Demonstrate Denied Privileged Syscalls:**
   ```bash
   # Try to change hostname inside container (requires CAP_SYS_ADMIN)
   sudo ./bin/minic run ./rootfs /bin/sh -c '/bin/hostname hacked'
   ```
   - **Expected Output:**
     `hostname: sethostname: Operation not permitted`

   ```bash
   # Try to create a raw ICMP socket (requires CAP_NET_RAW)
   sudo ./bin/minic run ./rootfs /bin/sh -c '/bin/busybox ping -c 1 127.0.0.1'
   ```
   - **Expected Output:**
     `ping: permission denied (are you root?)`

   ```bash
   # Try to mount a filesystem (requires CAP_SYS_ADMIN)
   sudo ./bin/minic run ./rootfs /bin/sh -c '/bin/busybox mount -t tmpfs none /mnt'
   ```
   - **Expected Output:**
     `mount: permission denied (are you root?)`

- **OS Concept:** In Linux, root privilege is governed by capabilities, not simply `UID == 0`. Stripping the bounding set, ambient set, and effective/permitted sets renders the container process unprivileged. `PR_SET_NO_NEW_PRIVS` ensures setuid binaries cannot regain capabilities across `execve`.

---

### Demo 7: Lifecycle, Signal Handling & Automatic Cleanup
**Objective:** Show that exit codes propagate accurately, signals are handled cleanly, and all temporary mounts and cgroups are automatically destroyed.

1. **Exit Code Propagation:**
   ```bash
   sudo ./bin/minic run ./rootfs /bin/sh -c 'exit 42'
   echo "Captured Exit Code: $?"
   ```
   - **Expected Output:**
     `Captured Exit Code: 42`

2. **Automatic Cleanup Verification:**
   ```bash
   # Check that no lingering mounts remain
   grep -E 'minic-run' /proc/mounts || echo "PASS: No stale mounts."

   # Check that no lingering cgroups remain
   ls /sys/fs/cgroup/minic-* 2>/dev/null || echo "PASS: No stale cgroups."

   # Check that no staging directories remain in /tmp
   ls -d /tmp/minic-run-* 2>/dev/null || echo "PASS: No stale staging directories."
   ```
   - **Expected Output:** All three checks report `PASS`.
- **OS Concept:** Go's LIFO deferred cleanup functions guarantee that cgroup processes are terminated, cgroups are removed, and temporary OverlayFS staging mounts are unmounted and erased even if the container exits with an error code.

---

## Fallback Instructions

If running in an environment without full cgroups v2 controller delegation or OverlayFS support:
- Ensure the kernel version is $\ge 5.4$ (`uname -r`).
- Run under WSL 2 Ubuntu on Windows 11/10 (`wsl -d Ubuntu`).
- To test unprivileged compilation and unit tests without root:
  ```bash
  make check
  ```
