package filesystem

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func SetupRootfs(rootfs string, isDebug bool) error {
	info, err := os.Stat(rootfs)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("rootfs does not exist")
		}
		return fmt.Errorf("failed to stat rootfs: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("rootfs is not a directory")
	}

	if err := unix.Mount("", "/", "", unix.MS_PRIVATE|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("failed to make mount namespace private: %w", err)
	}

	if err := unix.Mount(rootfs, rootfs, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("failed to bind-mount rootfs: %w", err)
	}

	oldRoot := filepath.Join(rootfs, ".minic_oldroot")
	if err := os.MkdirAll(oldRoot, 0700); err != nil {
		return fmt.Errorf("failed to create old-root directory: %w", err)
	}

	if err := unix.PivotRoot(rootfs, oldRoot); err != nil {
		return fmt.Errorf("failed to pivot_root: %w", err)
	}
	if isDebug {
		fmt.Println("[MiniC] pivot_root completed")
	}

	if err := unix.Chdir("/"); err != nil {
		return fmt.Errorf("failed to chdir to new root: %w", err)
	}

	oldRootPath := "/.minic_oldroot"
	if err := unix.Unmount(oldRootPath, unix.MNT_DETACH); err != nil {
		return fmt.Errorf("failed to unmount old root: %w", err)
	}

	if err := os.Remove(oldRootPath); err != nil {
		return fmt.Errorf("failed to remove old root directory: %w", err)
	}

	if err := os.MkdirAll("/proc", 0755); err != nil {
		return fmt.Errorf("failed to create /proc directory: %w", err)
	}

	if err := unix.Mount("proc", "/proc", "proc", 0, ""); err != nil {
		return fmt.Errorf("failed to mount /proc: %w", err)
	}
	if isDebug {
		fmt.Println("[MiniC] Mounted /proc")
	}

	return nil
}
