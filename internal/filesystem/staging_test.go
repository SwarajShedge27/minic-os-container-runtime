package filesystem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageRootfs(t *testing.T) {
	tempBase := t.TempDir()
	srcRootfs := filepath.Join(tempBase, "source_rootfs")
	if err := os.MkdirAll(filepath.Join(srcRootfs, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcRootfs, "bin", "echo"), []byte("echo-bin"), 0755); err != nil {
		t.Fatal(err)
	}

	dirs, err := StageRootfsIn(tempBase, srcRootfs)
	if err != nil {
		t.Fatalf("StageRootfsIn failed: %v", err)
	}
	if dirs == nil {
		t.Fatal("expected non-nil OverlayDirs")
	}

	// Verify base directory permissions are restrictive (0700)
	baseInfo, err := os.Stat(dirs.Base)
	if err != nil {
		t.Fatalf("failed to stat staged base: %v", err)
	}
	if baseInfo.Mode().Perm() != 0700 {
		t.Errorf("expected base dir mode 0700, got %v", baseInfo.Mode())
	}

	// Verify all 4 required subdirectories exist
	for _, sub := range []string{dirs.Lower, dirs.Upper, dirs.Work, dirs.Merged} {
		info, err := os.Stat(sub)
		if err != nil {
			t.Errorf("expected sub dir %s to exist: %v", sub, err)
		} else if !info.IsDir() {
			t.Errorf("expected %s to be a directory", sub)
		}
	}

	// Verify content was copied to lower
	stagedEcho := filepath.Join(dirs.Lower, "bin", "echo")
	if _, err := os.Stat(stagedEcho); err != nil {
		t.Errorf("expected staged echo binary in lower: %v", err)
	}

	// Verify cleanup
	if err := dirs.Cleanup(); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	if _, err := os.Stat(dirs.Base); !os.IsNotExist(err) {
		t.Errorf("expected base directory %s to be deleted after Cleanup", dirs.Base)
	}
}

func TestStageRootfsSafety(t *testing.T) {
	tempBase := t.TempDir()

	// Non-existent source rootfs
	_, err := StageRootfsIn(tempBase, filepath.Join(tempBase, "nonexistent"))
	if err == nil {
		t.Fatal("expected error staging non-existent rootfs")
	}

	// Safety check: refusing to clean up root or system directories
	unsafeDirs := []string{"/", "/tmp", "/var", "."}
	for _, unsafePath := range unsafeDirs {
		d := &OverlayDirs{Base: unsafePath}
		err := d.Cleanup()
		if err == nil {
			t.Errorf("expected safety error when cleaning up %s, got nil", unsafePath)
		}
		if !strings.Contains(err.Error(), "refusing to remove") {
			t.Errorf("expected safety rejection message for %s, got: %v", unsafePath, err)
		}
	}

	// Rejection of directory without minic- prefix
	arbitraryDir := filepath.Join(tempBase, "some-random-folder")
	_ = os.MkdirAll(arbitraryDir, 0755)
	dArbitrary := &OverlayDirs{Base: arbitraryDir}
	err = dArbitrary.Cleanup()
	if err == nil {
		t.Fatal("expected error cleaning up directory without minic prefix")
	}
	if !strings.Contains(err.Error(), "refusing to remove unrecognized") {
		t.Errorf("expected unrecognized directory error, got: %v", err)
	}
}
