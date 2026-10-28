package filesystem

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CopyRootfs copies a root filesystem tree from src to dst.
// It preserves directory hierarchy, regular file contents, file permissions,
// and symbolic links without dereferencing them.
// Unsupported special files (e.g. character/block devices, sockets, FIFOs)
// are reported as errors.
func CopyRootfs(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("source rootfs does not exist: %s", src)
		}
		return fmt.Errorf("failed to stat source rootfs: %w", err)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("source rootfs is not a directory: %s", src)
	}

	cleanSrc := filepath.Clean(src)
	cleanDst := filepath.Clean(dst)

	return filepath.Walk(cleanSrc, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if path == cleanSrc {
			return os.MkdirAll(cleanDst, 0755)
		}

		rel, err := filepath.Rel(cleanSrc, path)
		if err != nil {
			return fmt.Errorf("failed to determine relative path for %s: %w", path, err)
		}

		target := filepath.Join(cleanDst, rel)

		// Check for symbolic link first (info from Walk is from Lstat)
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("failed to read symlink %s: %w", path, err)
			}
			_ = os.Remove(target)
			if err := os.Symlink(linkTarget, target); err != nil {
				return fmt.Errorf("failed to create symlink %s -> %s: %w", target, linkTarget, err)
			}
			return nil
		}

		// Handle directory
		if info.IsDir() {
			perm := info.Mode().Perm()
			if perm == 0 {
				perm = 0755
			}
			return os.MkdirAll(target, perm)
		}

		// Handle regular file
		if info.Mode().IsRegular() {
			mode := info.Mode().Perm()
			// Under Windows-backed filesystems (9p/drvfs), Linux execute bits may not
			// reflect Git tracked modes accurately. Ensure busybox and executables have 0755.
			if filepath.Base(path) == "busybox" || (mode&0111 != 0) {
				mode = 0755
			}
			return copyRegularFile(path, target, mode)
		}

		// Detect and report unsupported object types
		return fmt.Errorf("unsupported filesystem object type in rootfs at %s (mode: %v)", path, info.Mode())
	})
}

func copyRegularFile(src, dst string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", src, err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("failed to create target file %s: %w", dst, err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file contents from %s to %s: %w", src, dst, err)
	}

	return nil
}
