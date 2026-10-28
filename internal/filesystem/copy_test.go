package filesystem

import (
	"os"
	"path/filepath"
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
