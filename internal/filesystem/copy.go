package filesystem

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// V9FSMagic represents the magic number for 9P/DrvFs filesystems in Linux (0x01021997).
const V9FSMagic = 0x01021997

// IsDrvFs reports whether the given path resides on a Windows DrvFs / 9P filesystem.
func IsDrvFs(path string) (bool, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return false, fmt.Errorf("statfs failed for %s: %w", path, err)
	}
	return st.Type == V9FSMagic, nil
}

type dirRecord struct {
	path string
	uid  int
	gid  int
	mode os.FileMode
}

// CopyRootfs copies a root filesystem tree from src to dst.
// It preserves directory hierarchy, regular file contents, file permissions,
// ownership (UID and GID on filesystems that support Unix ownership),
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

	// Determine whether source filesystem supports genuine Unix ownership.
	// On Windows DrvFs mounts (9p), ownership is synthetically mapped (typically 1000:1000).
	// We only preserve source ownership when the source is on a native Unix filesystem.
	isDrvFs, err := IsDrvFs(cleanSrc)
	if err != nil {
		return fmt.Errorf("failed to inspect filesystem type for %s: %w", cleanSrc, err)
	}
	preserveOwnership := !isDrvFs

	var dirsToFinalize []dirRecord

	walkErr := filepath.Walk(cleanSrc, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		var srcUID, srcGID int
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			srcUID = int(stat.Uid)
			srcGID = int(stat.Gid)
		}

		if path == cleanSrc {
			if err := os.MkdirAll(cleanDst, 0755); err != nil {
				return fmt.Errorf("failed to create destination root directory %s: %w", cleanDst, err)
			}
			dirsToFinalize = append(dirsToFinalize, dirRecord{
				path: cleanDst,
				uid:  srcUID,
				gid:  srcGID,
				mode: info.Mode(),
			})
			return nil
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
			if preserveOwnership {
				if err := os.Lchown(target, srcUID, srcGID); err != nil {
					return fmt.Errorf("failed to lchown symlink %s: %w", target, err)
				}
			}
			return nil
		}

		// Handle directory: create with accessible permissions so sub-items can be populated.
		// Final ownership and restrictive permissions are applied in reverse post-order.
		if info.IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", target, err)
			}
			dirsToFinalize = append(dirsToFinalize, dirRecord{
				path: target,
				uid:  srcUID,
				gid:  srcGID,
				mode: info.Mode(),
			})
			return nil
		}

		// Handle regular file
		if info.Mode().IsRegular() {
			mode := info.Mode().Perm()
			// Under Windows-backed filesystems (9p/drvfs), Linux execute bits may not
			// reflect Git tracked modes accurately. Ensure busybox and executables have 0755.
			if filepath.Base(path) == "busybox" || (mode&0111 != 0) {
				mode = 0755
			}
			if err := copyRegularFile(path, target, mode); err != nil {
				return err
			}
			if preserveOwnership {
				if err := os.Lchown(target, srcUID, srcGID); err != nil {
					return fmt.Errorf("failed to lchown file %s: %w", target, err)
				}
			}
			return nil
		}

		// Detect and report unsupported object types
		return fmt.Errorf("unsupported filesystem object type in rootfs at %s (mode: %v)", path, info.Mode())
	})

	if walkErr != nil {
		return walkErr
	}

	// Finalize directory ownership and permissions in reverse order (deepest directories first).
	// This ensures that restrictive directory permissions (e.g. 0700) or non-root ownership
	// do not impede copying child entries during traversal.
	for i := len(dirsToFinalize) - 1; i >= 0; i-- {
		d := dirsToFinalize[i]
		if preserveOwnership {
			if err := os.Lchown(d.path, d.uid, d.gid); err != nil {
				return fmt.Errorf("failed to lchown directory %s: %w", d.path, err)
			}
		}
		perm := d.mode.Perm()
		if d.mode&os.ModeSticky != 0 {
			perm |= os.ModeSticky
		}
		if err := os.Chmod(d.path, perm); err != nil {
			return fmt.Errorf("failed to chmod directory %s: %w", d.path, err)
		}
	}

	return nil
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
