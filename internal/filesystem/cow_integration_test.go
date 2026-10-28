package filesystem

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// TestPrivilegedOverlayCOW verifies the kernel OverlayFS copy-on-write mechanism.
// It is automatically skipped when running unprivileged unit tests.
func TestPrivilegedOverlayCOW(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("skipping privileged OverlayFS integration test: requires root (run with sudo)")
	}

	tempBase := t.TempDir()
	sourceRoot := filepath.Join(tempBase, "source_rootfs")
	if err := os.MkdirAll(sourceRoot, 0755); err != nil {
		t.Fatal(err)
	}

	initialFile := filepath.Join(sourceRoot, "original.txt")
	originalContent := "initial-content"
	if err := os.WriteFile(initialFile, []byte(originalContent), 0644); err != nil {
		t.Fatal(err)
	}

	dirs, err := StageRootfsIn(tempBase, sourceRoot)
	if err != nil {
		t.Fatalf("StageRootfsIn failed: %v", err)
	}
	defer func() {
		_ = unix.Unmount(dirs.Merged, unix.MNT_DETACH)
		_ = dirs.Cleanup()
	}()

	// Mount OverlayFS onto merged
	opts := BuildOverlayOptions(dirs.Lower, dirs.Upper, dirs.Work)
	if err := unix.Mount("overlay", dirs.Merged, "overlay", 0, opts); err != nil {
		t.Fatalf("failed to mount overlayfs: %v", err)
	}

	// 1. Verify original file is readable in merged
	mergedOriginal := filepath.Join(dirs.Merged, "original.txt")
	data, err := os.ReadFile(mergedOriginal)
	if err != nil || string(data) != originalContent {
		t.Fatalf("expected merged original to have content %q, got %q (err=%v)", originalContent, string(data), err)
	}

	// 2. Modify original file in merged
	modifiedContent := "modified-content-in-container"
	if err := os.WriteFile(mergedOriginal, []byte(modifiedContent), 0644); err != nil {
		t.Fatalf("failed to write to merged file: %v", err)
	}

	// 3. Create a brand new file in merged
	mergedNew := filepath.Join(dirs.Merged, "new_file.txt")
	newContent := "new-file-in-container"
	if err := os.WriteFile(mergedNew, []byte(newContent), 0644); err != nil {
		t.Fatalf("failed to create new file in merged: %v", err)
	}

	// 4. Verify lower layer (staged copy) and original source rootfs remain completely untouched!
	lowerOriginal := filepath.Join(dirs.Lower, "original.txt")
	lowerData, err := os.ReadFile(lowerOriginal)
	if err != nil || string(lowerData) != originalContent {
		t.Errorf("lower layer was modified! Expected %q, got %q", originalContent, string(lowerData))
	}

	sourceData, err := os.ReadFile(initialFile)
	if err != nil || string(sourceData) != originalContent {
		t.Errorf("source rootfs was modified! Expected %q, got %q", originalContent, string(sourceData))
	}

	if _, err := os.Stat(filepath.Join(sourceRoot, "new_file.txt")); !os.IsNotExist(err) {
		t.Errorf("new file leaked into source rootfs!")
	}

	// 5. Verify the modifications exist strictly in the upper layer
	upperOriginal := filepath.Join(dirs.Upper, "original.txt")
	upperData, err := os.ReadFile(upperOriginal)
	if err != nil || string(upperData) != modifiedContent {
		t.Errorf("expected modified file in upper layer with %q, got %q", modifiedContent, string(upperData))
	}

	upperNew := filepath.Join(dirs.Upper, "new_file.txt")
	upperNewData, err := os.ReadFile(upperNew)
	if err != nil || string(upperNewData) != newContent {
		t.Errorf("expected new file in upper layer with %q, got %q", newContent, string(upperNewData))
	}
}
