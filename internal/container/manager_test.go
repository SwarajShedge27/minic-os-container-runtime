package container

import (
	"testing"
)

func TestManagerInitialState(t *testing.T) {
	cfg := &Config{
		MemoryLimit: "100M",
		CPULimit:    50,
		PIDLimit:    20,
		Hostname:    "test-box",
		Debug:       true,
		RootFS:      "./rootfs",
		Command:     "/bin/sh",
	}

	mgr := NewManager(cfg)
	if mgr == nil {
		t.Fatal("expected non-nil Manager")
	}
	if mgr.State() != Created {
		t.Errorf("expected initial state %q, got %q", Created, mgr.State())
	}
}
