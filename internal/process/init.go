package process

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"golang.org/x/sys/unix"
	"minic/internal/filesystem"
	"minic/internal/network"
)

// Init initializes the container environment inside isolated namespaces.
// It synchronizes with the parent, mounts the OverlayFS root with device files,
// sets the container hostname, and executes the user command.
func Init(syncFd, merged, lower, upper, work, hostname, command string, args []string) error {
	isDebug := os.Getenv("MINIC_DEBUG") == "true"

	fd, err := strconv.Atoi(syncFd)
	if err != nil || fd < 0 {
		return fmt.Errorf("invalid sync file descriptor: %s", syncFd)
	}

	syncPipe := os.NewFile(uintptr(fd), "sync-pipe")
	if syncPipe == nil {
		return fmt.Errorf("failed to open sync pipe on FD %d", fd)
	}

	buf := make([]byte, 1)
	n, err := syncPipe.Read(buf)
	if err != nil {
		syncPipe.Close()
		return fmt.Errorf("failed to read from sync pipe: %w", err)
	}
	if n != 1 {
		syncPipe.Close()
		return fmt.Errorf("unexpected sync byte count: %d", n)
	}

	// Explicitly close synchronization descriptor BEFORE filesystem setup and command execution.
	if err := syncPipe.Close(); err != nil {
		return fmt.Errorf("failed to close sync pipe: %w", err)
	}

	if isDebug {
		fmt.Println("[MiniC] Preparing OverlayFS rootfs")
	}

	if err := filesystem.SetupOverlayRootfs(lower, upper, work, merged, isDebug); err != nil {
		return fmt.Errorf("failed to prepare overlay rootfs: %w", err)
	}

	if isDebug {
		fmt.Println("[MiniC] Configuring network loopback interface")
	}

	if err := network.SetupLoopback(isDebug); err != nil {
		return fmt.Errorf("failed to configure network loopback: %w", err)
	}

	if hostname != "" {
		if err := unix.Sethostname([]byte(hostname)); err != nil {
			return fmt.Errorf("failed to set hostname: %w", err)
		}
		if isDebug {
			fmt.Println("[MiniC] Hostname configured")
		}
	}

	path, err := exec.LookPath(command)
	if err != nil {
		return fmt.Errorf("executable not found: %w", err)
	}

	if isDebug {
		fmt.Printf("[MiniC] Executing %s\n", path)
	}

	argv := append([]string{command}, args...)
	if err := unix.Exec(path, argv, os.Environ()); err != nil {
		return fmt.Errorf("failed to execute command: %w", err)
	}

	return nil
}
