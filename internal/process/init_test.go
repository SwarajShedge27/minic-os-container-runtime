package process

import (
	"strings"
	"testing"
)

func TestInitValidation(t *testing.T) {
	// Test invalid sync file descriptor string
	err := Init("invalid-fd", "/tmp/m", "/tmp/l", "/tmp/u", "/tmp/w", "minic", "/bin/echo", nil)
	if err == nil {
		t.Fatal("expected error for non-numeric syncFd")
	}
	if !strings.Contains(err.Error(), "invalid sync file descriptor") {
		t.Errorf("expected error message about invalid sync file descriptor, got: %v", err)
	}

	// Test negative sync file descriptor
	err = Init("-5", "/tmp/m", "/tmp/l", "/tmp/u", "/tmp/w", "minic", "/bin/echo", nil)
	if err == nil {
		t.Fatal("expected error for negative syncFd")
	}
	if !strings.Contains(err.Error(), "invalid sync file descriptor") {
		t.Errorf("expected error message about invalid sync file descriptor, got: %v", err)
	}
}
