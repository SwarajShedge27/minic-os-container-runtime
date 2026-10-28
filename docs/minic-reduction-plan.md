# MiniC Reduction Plan

This document outlines the step-by-step plan to build the standalone `MiniC` runtime from scratch, drawing architectural inspiration from `opencontainers/runc`.

**Important**: MiniC is built as a clean, independent Go project in the `minic/` directory. The `runc` source code is treated as a read-only reference.

## 1. Initial State (Runc)
The cloned `runc` repository contains an extensive OCI-compliant runtime that supports multiple container states, checkpoint/restore, cgroups v1/v2 across diverse distributions, rootless execution, advanced security (AppArmor, SELinux, Seccomp), and C-based namespace setup before Go runtime execution.

## 2. Implementation Strategy
We will implement MiniC in the `minic/` subdirectory, relying on `golang.org/x/sys/unix` for syscalls and keeping the state model minimal.

### Step 1: Scaffolding and CLI
- Create `cmd/minic/main.go`.
- Implement a basic CLI parser using the standard `flag` package to parse `minic run [options] <rootfs> <command>`.

### Step 2: Parent/Child Process Execution Model
- Implement `internal/container/manager.go`.
- Create a mechanism for the MiniC parent process to invoke itself (e.g., `minic child`) to spawn the isolated process using `os/exec`.
- Track minimal state: `Created`, `Starting`, `Running`, `Stopped`.

### Step 3: Namespace Isolation
- Configure the `os/exec.Cmd` in the Parent process to use `unix.SysProcAttr{Cloneflags: unix.CLONE_NEWPID | unix.CLONE_NEWNS | unix.CLONE_NEWUTS}`.

### Step 4: Parent-Child Synchronization
- Implement `internal/sync/pipe.go` to create an `os.Pipe` between the parent and child.
- The child blocks on the read end immediately upon starting.
- The parent configures cgroups, writes the PID, and then signals the child.

### Step 5: Cgroups Management
- Implement `internal/cgroup/cgroup.go`.
- The parent process handles creating `/sys/fs/cgroup/minic_<id>` and populates limits based on CLI flags.
- The parent writes the child's PID to `cgroup.procs` and then signals the synchronization pipe to wake up the child.

### Step 6: Filesystem Isolation (Child)
- Implement `internal/filesystem/mount.go` for the child process.
- Change mount propagation to private.
- Bind mount the user-provided local rootfs.
- Mount `/proc`.
- Call `unix.PivotRoot()`. Check errors carefully and never perform destructive cleanups on host paths.

### Step 7: Final Execution (Child)
- The child sets the hostname via `unix.Sethostname`.
- The child resolves the target executable path and invokes `unix.Exec()`.

### Step 8: Cleanup and Reporting (Parent)
- The parent process waits for the child's completion.
- Upon child exit, the parent deletes the cgroup directory.
- Implement the `--debug` diagnostic mode to print lifecycle logs to `stdout`.

## 3. Coherence Check
By following this step-by-step implementation plan, MiniC will fulfill all the OS-level container isolation goals set out in the PRD while remaining small, independent, and easily understandable for an educational context.
