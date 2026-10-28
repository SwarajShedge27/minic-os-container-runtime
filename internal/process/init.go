package process

import (
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
	"minic/internal/filesystem"
)

func Init(syncFd string, rootfs string, hostname string, command string, args []string) error {
	isDebug := os.Getenv("MINIC_DEBUG") == "true"

	syncPipe := os.NewFile(uintptr(3), "sync-pipe")
	if syncPipe == nil {
		return fmt.Errorf("failed to open sync pipe on FD 3")
	}
	defer syncPipe.Close()

	buf := make([]byte, 1)
	if _, err := syncPipe.Read(buf); err != nil {
		return fmt.Errorf("failed to read from sync pipe: %w", err)
	}

	if isDebug {
		fmt.Println("[MiniC] Preparing rootfs")
	}

	if err := filesystem.SetupRootfs(rootfs, isDebug); err != nil {
		return fmt.Errorf("failed to prepare rootfs: %w", err)
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
