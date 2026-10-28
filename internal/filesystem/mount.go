package filesystem

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// RequiredDevices lists the essential device files provisioned into the container root.
var RequiredDevices = []string{"null", "zero", "random", "urandom"}

// BuildOverlayOptions constructs the options string for the OverlayFS mount syscall.
func BuildOverlayOptions(lower, upper, work string) string {
	return fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lower, upper, work)
}

// SetupOverlayRootfs sets up an isolated container root filesystem using OverlayFS.
// It executes inside the child process's isolated mount namespace.
// Sequence:
//  1. Sets mount namespace propagation to private.
//  2. Mounts OverlayFS onto the merged directory.
//  3. Bind-mounts merged onto itself to make it a distinct mount point.
//  4. Provisions essential device files (/dev/null, /dev/zero, /dev/random, /dev/urandom).
//  5. Pivots into the merged root (unix.PivotRoot).
//  6. Changes working directory to new root ("/").
//  7. Detaches and removes the old host root mount.
//  8. Mounts the virtual /proc filesystem.
func SetupOverlayRootfs(lower, upper, work, merged string, isDebug bool) error {
	// Validate that all required overlay directories exist
	for _, dir := range []struct {
		name string
		path string
	}{
		{"lowerdir", lower},
		{"upperdir", upper},
		{"workdir", work},
		{"merged", merged},
	} {
		info, err := os.Stat(dir.path)
		if err != nil {
			return fmt.Errorf("%s does not exist: %s (%w)", dir.name, dir.path, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory: %s", dir.name, dir.path)
		}
	}

	// 1. Make mount namespace private so mounts do not propagate back to host
	if err := unix.Mount("", "/", "", unix.MS_PRIVATE|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("failed to make mount namespace private: %w", err)
	}

	// 2. Mount OverlayFS onto the merged directory
	opts := BuildOverlayOptions(lower, upper, work)
	if err := unix.Mount("overlay", merged, "overlay", 0, opts); err != nil {
		return fmt.Errorf("failed to mount overlayfs on %s: %w", merged, err)
	}
	if isDebug {
		fmt.Println("[MiniC] Mounted OverlayFS root")
	}

	// 3. Bind-mount merged onto itself to satisfy pivot_root requirement
	if err := unix.Mount(merged, merged, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("failed to bind-mount merged overlay: %w", err)
	}

	// 4. Provision minimal device files into <merged>/dev
	if err := setupDevices(merged, isDebug); err != nil {
		return fmt.Errorf("failed to provision device files: %w", err)
	}

	// 5. Create temporary oldroot directory inside the disposable merged root
	oldRoot := filepath.Join(merged, ".minic_oldroot")
	if err := os.MkdirAll(oldRoot, 0700); err != nil {
		return fmt.Errorf("failed to create old-root directory: %w", err)
	}

	// 6. Pivot root: swap host root with merged overlay root
	if err := unix.PivotRoot(merged, oldRoot); err != nil {
		return fmt.Errorf("failed to pivot_root: %w", err)
	}
	if isDebug {
		fmt.Println("[MiniC] pivot_root completed")
	}

	// 7. Change directory to new container root
	if err := unix.Chdir("/"); err != nil {
		return fmt.Errorf("failed to chdir to new root: %w", err)
	}

	// 8. Detach and remove old host root
	oldRootPath := "/.minic_oldroot"
	if err := unix.Unmount(oldRootPath, unix.MNT_DETACH); err != nil {
		return fmt.Errorf("failed to unmount old root: %w", err)
	}
	if err := os.Remove(oldRootPath); err != nil {
		return fmt.Errorf("failed to remove old root directory: %w", err)
	}

	// 9. Mount /proc
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

func setupDevices(mergedRoot string, isDebug bool) error {
	devDir := filepath.Join(mergedRoot, "dev")
	if err := os.MkdirAll(devDir, 0755); err != nil {
		return fmt.Errorf("failed to create /dev in container root: %w", err)
	}

	for _, devName := range RequiredDevices {
		hostPath := filepath.Join("/dev", devName)
		if _, err := os.Stat(hostPath); err != nil {
			return fmt.Errorf("required host device %s is not accessible: %w", hostPath, err)
		}

		targetPath := filepath.Join(devDir, devName)
		// Create placeholder regular file as mount point target
		f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDONLY, 0666)
		if err != nil {
			return fmt.Errorf("failed to create placeholder for device %s: %w", targetPath, err)
		}
		f.Close()

		// Bind-mount specific device node from host into container /dev
		if err := unix.Mount(hostPath, targetPath, "", unix.MS_BIND, ""); err != nil {
			return fmt.Errorf("failed to bind-mount device %s to %s: %w", hostPath, targetPath, err)
		}
	}

	if isDebug {
		fmt.Println("[MiniC] Provisioned minimal devices (/dev/null, /dev/zero, /dev/random, /dev/urandom)")
	}

	return nil
}
