package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultStagingBaseDir is the directory where per-run runtime directories are created.
// In WSL, /tmp resides on the Linux-native ext4/tmpfs filesystem.
var DefaultStagingBaseDir = "/tmp"

// OverlayDirs holds the absolute directory paths for an OverlayFS setup.
type OverlayDirs struct {
	Base   string // e.g. /tmp/minic-run-123456789
	Lower  string // e.g. /tmp/minic-run-123456789/lower (read-only copy of rootfs)
	Upper  string // e.g. /tmp/minic-run-123456789/upper (writable layer)
	Work   string // e.g. /tmp/minic-run-123456789/work (OverlayFS internal work dir)
	Merged string // e.g. /tmp/minic-run-123456789/merged (final container mount point)
}

// StageRootfs prepares a per-run isolated OverlayFS workspace on a Linux-native filesystem.
func StageRootfs(sourceRootfs string) (*OverlayDirs, error) {
	return StageRootfsIn(DefaultStagingBaseDir, sourceRootfs)
}

// StageRootfsIn allows specifying the base directory (useful for testing).
func StageRootfsIn(baseDir, sourceRootfs string) (*OverlayDirs, error) {
	if _, err := os.Stat(sourceRootfs); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("source rootfs does not exist: %s", sourceRootfs)
		}
		return nil, fmt.Errorf("failed to stat source rootfs: %w", err)
	}

	absSource, err := filepath.Abs(sourceRootfs)
	if err != nil {
		return nil, fmt.Errorf("failed to determine absolute path of source rootfs: %w", err)
	}

	// Create unique runtime base with restrictive 0700 permissions
	runtimeDir, err := os.MkdirTemp(baseDir, "minic-run-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary runtime directory in %s: %w", baseDir, err)
	}
	_ = os.Chmod(runtimeDir, 0700)

	dirs := &OverlayDirs{
		Base:   runtimeDir,
		Lower:  filepath.Join(runtimeDir, "lower"),
		Upper:  filepath.Join(runtimeDir, "upper"),
		Work:   filepath.Join(runtimeDir, "work"),
		Merged: filepath.Join(runtimeDir, "merged"),
	}

	// Create the required subdirectories
	for _, dir := range []string{dirs.Lower, dirs.Upper, dirs.Work, dirs.Merged} {
		if err := os.Mkdir(dir, 0755); err != nil {
			_ = os.RemoveAll(runtimeDir)
			return nil, fmt.Errorf("failed to create overlay subdirectory %s: %w", dir, err)
		}
	}

	// Copy source rootfs into the lower directory
	if err := CopyRootfs(absSource, dirs.Lower); err != nil {
		_ = os.RemoveAll(runtimeDir)
		return nil, fmt.Errorf("failed to copy rootfs to staging lowerdir: %w", err)
	}

	return dirs, nil
}

// Cleanup safely removes the temporary runtime directory.
func (d *OverlayDirs) Cleanup() error {
	if d == nil || d.Base == "" {
		return nil
	}

	cleanBase := filepath.Clean(d.Base)
	// Safety validation: ensure we never delete root or system directories
	if cleanBase == "/" || cleanBase == "." || cleanBase == "/tmp" || cleanBase == "/var" {
		return fmt.Errorf("refusing to remove unsafe base directory: %s", d.Base)
	}

	// Ensure the directory name has our expected prefix
	baseName := filepath.Base(cleanBase)
	if !strings.HasPrefix(baseName, "minic-run-") && !strings.HasPrefix(baseName, "minic-test-") {
		return fmt.Errorf("refusing to remove unrecognized runtime directory: %s", d.Base)
	}

	if err := os.RemoveAll(cleanBase); err != nil {
		return fmt.Errorf("failed to cleanup runtime directory %s: %w", cleanBase, err)
	}

	return nil
}
