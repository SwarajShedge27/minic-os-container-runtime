package filesystem

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildOverlayOptions(t *testing.T) {
	lower := "/tmp/minic-1/lower"
	upper := "/tmp/minic-1/upper"
	work := "/tmp/minic-1/work"

	expected := "lowerdir=/tmp/minic-1/lower,upperdir=/tmp/minic-1/upper,workdir=/tmp/minic-1/work"
	actual := BuildOverlayOptions(lower, upper, work)
	if actual != expected {
		t.Errorf("expected options %q, got %q", expected, actual)
	}
}

func TestSetupOverlayRootfsValidation(t *testing.T) {
	tempDir := t.TempDir()

	// Lowerdir missing
	err := SetupOverlayRootfs(filepath.Join(tempDir, "missing_lower"), tempDir, tempDir, tempDir, false)
	if err == nil {
		t.Fatal("expected error when lowerdir is missing")
	}
	if !strings.Contains(err.Error(), "lowerdir does not exist") {
		t.Errorf("expected lowerdir error, got: %v", err)
	}

	// Upperdir missing
	err = SetupOverlayRootfs(tempDir, filepath.Join(tempDir, "missing_upper"), tempDir, tempDir, false)
	if err == nil {
		t.Fatal("expected error when upperdir is missing")
	}
	if !strings.Contains(err.Error(), "upperdir does not exist") {
		t.Errorf("expected upperdir error, got: %v", err)
	}
}

func TestRequiredDevicesList(t *testing.T) {
	expected := map[string]bool{
		"null":    true,
		"zero":    true,
		"random":  true,
		"urandom": true,
	}

	if len(RequiredDevices) != len(expected) {
		t.Errorf("expected %d required devices, got %d", len(expected), len(RequiredDevices))
	}

	for _, dev := range RequiredDevices {
		if !expected[dev] {
			t.Errorf("unexpected device in RequiredDevices: %s", dev)
		}
	}
}
