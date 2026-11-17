package filesystem

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCopyRootfs(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "staged")

	// Create test structure:
	// src/
	//   bin/
	//     busybox (regular file, 0755)
	//     sh -> busybox (symlink)
	//   etc/
	//     config.txt (regular file, 0644)
	//   sub/
	//     nested/
	//       file.dat (regular file)
	binDir := filepath.Join(srcDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}

	busyboxPath := filepath.Join(binDir, "busybox")
	if err := os.WriteFile(busyboxPath, []byte("fake-busybox-binary"), 0755); err != nil {
		t.Fatal(err)
	}

	shPath := filepath.Join(binDir, "sh")
	if err := os.Symlink("busybox", shPath); err != nil {
		t.Fatal(err)
	}

	etcDir := filepath.Join(srcDir, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(etcDir, "config.txt")
	if err := os.WriteFile(configPath, []byte("key=value"), 0644); err != nil {
		t.Fatal(err)
	}

	nestedDir := filepath.Join(srcDir, "sub", "nested")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatal(err)
	}
	nestedFile := filepath.Join(nestedDir, "file.dat")
	if err := os.WriteFile(nestedFile, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}

	// Perform copy
	if err := CopyRootfs(srcDir, dstDir); err != nil {
		t.Fatalf("CopyRootfs failed: %v", err)
	}

	// Verify regular file content and permissions
	dstBusybox := filepath.Join(dstDir, "bin", "busybox")
	info, err := os.Stat(dstBusybox)
	if err != nil {
		t.Fatalf("expected staged busybox to exist: %v", err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Errorf("expected busybox to have executable bit, got %v", info.Mode())
	}
	content, err := os.ReadFile(dstBusybox)
	if err != nil || string(content) != "fake-busybox-binary" {
		t.Errorf("content mismatch for busybox: %s (err=%v)", string(content), err)
	}

	// Verify symlink is preserved as symlink, not copied as regular file
	dstSh := filepath.Join(dstDir, "bin", "sh")
	symInfo, err := os.Lstat(dstSh)
	if err != nil {
		t.Fatalf("expected staged sh to exist: %v", err)
	}
	if symInfo.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected staged sh to be a symlink, got %v", symInfo.Mode())
	}
	target, err := os.Readlink(dstSh)
	if err != nil || target != "busybox" {
		t.Errorf("expected symlink target 'busybox', got %q (err=%v)", target, err)
	}

	// Verify nested file
	dstNested := filepath.Join(dstDir, "sub", "nested", "file.dat")
	nestedContent, err := os.ReadFile(dstNested)
	if err != nil || string(nestedContent) != "data" {
		t.Errorf("expected nested file content 'data', got %q (err=%v)", string(nestedContent), err)
	}
}

func TestCopyRootfsErrors(t *testing.T) {
	tempDir := t.TempDir()

	// Non-existent source
	err := CopyRootfs(filepath.Join(tempDir, "nonexistent"), filepath.Join(tempDir, "dst"))
	if err == nil {
		t.Fatal("expected error for non-existent source rootfs")
	}

	// Source is a regular file, not a directory
	filePath := filepath.Join(tempDir, "regular.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	err = CopyRootfs(filePath, filepath.Join(tempDir, "dst"))
	if err == nil {
		t.Fatal("expected error when source is a file instead of directory")
	}
}

func TestIsDrvFs(t *testing.T) {
	tempDir := t.TempDir()
	isDrv, err := IsDrvFs(tempDir)
	if err != nil {
		t.Fatalf("IsDrvFs failed on temp dir: %v", err)
	}
	// Under native Linux storage or tmpfs, IsDrvFs should be false
	t.Logf("tempDir=%s isDrvFs=%v", tempDir, isDrv)
}

func TestCopyRootfsOwnershipAndPermissions(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "staged")

	// Create a restrictive directory (0700) with a child file
	restrictiveDir := filepath.Join(srcDir, "restricted")
	if err := os.MkdirAll(restrictiveDir, 0700); err != nil {
		t.Fatal(err)
	}
	childFile := filepath.Join(restrictiveDir, "child.txt")
	if err := os.WriteFile(childFile, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}

	// Create a symlink
	linkPath := filepath.Join(srcDir, "link_to_child")
	if err := os.Symlink("restricted/child.txt", linkPath); err != nil {
		t.Fatal(err)
	}

	if err := CopyRootfs(srcDir, dstDir); err != nil {
		t.Fatalf("CopyRootfs failed: %v", err)
	}

	// Verify restrictive directory mode was preserved
	stagedDirInfo, err := os.Stat(filepath.Join(dstDir, "restricted"))
	if err != nil {
		t.Fatalf("failed to stat staged directory: %v", err)
	}
	if stagedDirInfo.Mode().Perm() != 0700 {
		t.Errorf("expected directory mode 0700, got %v", stagedDirInfo.Mode().Perm())
	}

	// Verify child file mode was preserved
	stagedFileInfo, err := os.Stat(filepath.Join(dstDir, "restricted", "child.txt"))
	if err != nil {
		t.Fatalf("failed to stat staged child file: %v", err)
	}
	if stagedFileInfo.Mode().Perm() != 0600 {
		t.Errorf("expected file mode 0600, got %v", stagedFileInfo.Mode().Perm())
	}

	// Verify symlink target was not modified
	target, err := os.Readlink(filepath.Join(dstDir, "link_to_child"))
	if err != nil {
		t.Fatalf("failed to read staged symlink: %v", err)
	}
	if target != "restricted/child.txt" {
		t.Errorf("expected symlink target 'restricted/child.txt', got %q", target)
	}
}

func TestPrivilegedCopyRootfsNonRootOwnership(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("skipping privileged ownership preservation test: requires root (run with sudo)")
	}

	srcDir := filepath.Join("/tmp", "minic-test-ownership-src")
	dstDir := filepath.Join("/tmp", "minic-test-ownership-dst")
	_ = os.RemoveAll(srcDir)
	_ = os.RemoveAll(dstDir)
	defer func() {
		_ = os.RemoveAll(srcDir)
		_ = os.RemoveAll(dstDir)
	}()

	const targetUID = 70
	const targetGID = 70

	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a directory owned by UID 70, GID 70 with mode 0700
	pgDataDir := filepath.Join(srcDir, "pgdata")
	if err := os.MkdirAll(pgDataDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Lchown(pgDataDir, targetUID, targetGID); err != nil {
		t.Fatal(err)
	}

	// Create a file owned by UID 70, GID 70 with mode 0600 inside pgdata
	pgConfigFile := filepath.Join(pgDataDir, "postgresql.conf")
	if err := os.WriteFile(pgConfigFile, []byte("port = 15432\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Lchown(pgConfigFile, targetUID, targetGID); err != nil {
		t.Fatal(err)
	}

	// Create a symlink owned by UID 70, GID 70 pointing to the config file
	symlinkPath := filepath.Join(srcDir, "config_symlink")
	if err := os.Symlink("pgdata/postgresql.conf", symlinkPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Lchown(symlinkPath, targetUID, targetGID); err != nil {
		t.Fatal(err)
	}

	// Run CopyRootfs from native Linux filesystem (/tmp)
	if err := CopyRootfs(srcDir, dstDir); err != nil {
		t.Fatalf("CopyRootfs failed to copy test structure: %v", err)
	}

	// 1. Verify directory ownership and permissions
	stagedDirStat, err := os.Stat(filepath.Join(dstDir, "pgdata"))
	if err != nil {
		t.Fatalf("failed to stat staged pgdata dir: %v", err)
	}
	if stagedDirStat.Mode().Perm() != 0700 {
		t.Errorf("expected staged dir perm 0700, got %v", stagedDirStat.Mode().Perm())
	}
	dirSys := stagedDirStat.Sys().(*syscall.Stat_t)
	if int(dirSys.Uid) != targetUID || int(dirSys.Gid) != targetGID {
		t.Errorf("expected staged dir ownership %d:%d, got %d:%d", targetUID, targetGID, dirSys.Uid, dirSys.Gid)
	}

	// 2. Verify file ownership and permissions
	stagedFileStat, err := os.Stat(filepath.Join(dstDir, "pgdata", "postgresql.conf"))
	if err != nil {
		t.Fatalf("failed to stat staged config file: %v", err)
	}
	if stagedFileStat.Mode().Perm() != 0600 {
		t.Errorf("expected staged file perm 0600, got %v", stagedFileStat.Mode().Perm())
	}
	fileSys := stagedFileStat.Sys().(*syscall.Stat_t)
	if int(fileSys.Uid) != targetUID || int(fileSys.Gid) != targetGID {
		t.Errorf("expected staged file ownership %d:%d, got %d:%d", targetUID, targetGID, fileSys.Uid, fileSys.Gid)
	}

	// 3. Verify symlink ownership using Lstat (must NOT dereference)
	stagedSymlinkStat, err := os.Lstat(filepath.Join(dstDir, "config_symlink"))
	if err != nil {
		t.Fatalf("failed to lstat staged symlink: %v", err)
	}
	if stagedSymlinkStat.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected staged entry to be a symlink")
	}
	symlinkSys := stagedSymlinkStat.Sys().(*syscall.Stat_t)
	if int(symlinkSys.Uid) != targetUID || int(symlinkSys.Gid) != targetGID {
		t.Errorf("expected staged symlink ownership %d:%d, got %d:%d", targetUID, targetGID, symlinkSys.Uid, symlinkSys.Gid)
	}
}
