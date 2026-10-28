# Runc to MiniC Mapping

This document describes how the complex architecture of `opencontainers/runc` maps to the simplified educational model of MiniC.

**Important**: MiniC is a completely independent Go project. The `runc` repository is left untouched and used purely as a reference.

## 1. CLI and Configuration
- **Runc**: Uses `urfave/cli` for complex subcommands (`run`, `create`, `start`, `exec`, `state`, etc.) and reads an OCI `config.json` via `opencontainers/runtime-spec`.
- **MiniC**: Will use standard Go `flag` or a simple CLI router to support a single `minic run` command. The configuration will be parsed directly from CLI flags (e.g., `--memory 100M`) and passed internally. OCI spec parsing is completely removed.

## 2. Container Creation and Process Management
- **Runc**: Uses `libcontainer.Create()` to build a `linuxContainer` object. It creates a `parentProcess` which sets up synchronization pipes, creates an `exec.Cmd` to call `runc init`, and handles complex lifecycle states (PAUSED, STOPPED, RUNNING).
- **MiniC**: The `Container Manager (Parent)` will directly instantiate an `os/exec.Cmd` pointing to `/proc/self/exe` with a `child` argument. It will use an `os.Pipe` for parent-child synchronization. State management is minimal, tracking only `Created`, `Starting`, `Running`, and `Stopped`.

## 3. Namespace Isolation
- **Runc**: Because Go's multi-threaded runtime makes changing namespaces via `setns()` dangerous, runc uses a C-language package (`libcontainer/nsenter`) which runs as a constructor (`__attribute__((constructor))`) before the Go runtime initializes.
- **MiniC**: To avoid CGO and `nsenter`, MiniC will use Go's built-in `unix.SysProcAttr{Cloneflags: ...}` from `golang.org/x/sys/unix` when spawning the child process. The child will boot directly into the isolated namespaces (PID, Mount, UTS).

## 4. Cgroups Management
- **Runc**: Uses the `opencontainers/cgroups` external module, which supports cgroups v1, v2, systemd integration, rootless cgroups, and complex resource managers.
- **MiniC**: Will completely replace the external dependency with simple file I/O operations targeting the `/sys/fs/cgroup` directory. MiniC will strictly assume cgroups v2 and implement only `cpu.max`, `memory.max`, `pids.max`, and `cgroup.procs`.

## 5. Filesystem and `pivot_root`
- **Runc**: Handled in `libcontainer/rootfs_linux.go` and `standard_init_linux.go`. It handles mounting complex OCI volumes, `/dev` setup (mknod), overlayfs, mask paths, readonly paths, and `pivot_root`.
- **MiniC**: A drastically simplified `internal/filesystem` package. The child process will:
  1. Change mount propagation to private (`MS_PRIVATE | MS_REC` on `/`).
  2. Bind-mount the local `rootfs` to itself (a requirement for `pivot_root`).
  3. Mount `proc` at `<rootfs>/proc`.
  4. Call `unix.PivotRoot()`.
  5. Unmount the old root.

## 6. Execution (execve)
- **Runc**: The `init` process cleans up file descriptors, applies Seccomp profiles, drops capabilities, sets AppArmor/SELinux profiles, and finally calls `unix.Exec()`.
- **MiniC**: Will skip Seccomp, Capabilities, and MAC (AppArmor/SELinux) for the MVP. It will simply set the hostname, wait for the parent to assign the cgroup via pipe, and execute `unix.Exec()`.

## Summary Table

| Runc Component | MiniC Treatment |
|---|---|
| `main.go` / CLI | **Simplify** (Basic CLI flags) |
| OCI Spec Parsing | **Replace** (Direct CLI to struct) |
| `nsenter` (C Code) | **Remove** (Use Go `SysProcAttr.Cloneflags`) |
| `libcontainer/container_linux.go` | **Simplify** (Direct `exec.Cmd` parent) |
| `github.com/opencontainers/cgroups` | **Reimplement** (Simple file I/O for v2) |
| `standard_init_linux.go` | **Simplify** (`minic child` entrypoint) |
| `rootfs_linux.go` (`pivot_root`) | **Simplify** (Only basic mounts + `unix.PivotRoot`) |
| Seccomp / AppArmor / Capabilities | **Remove** (Educational MVP scope) |
| Network / User / IPC Namespaces | **Remove** initially (Optional later) |
