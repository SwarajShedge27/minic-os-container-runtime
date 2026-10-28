package container

import (
	"testing"

	"golang.org/x/sys/unix"
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

func TestDefaultCloneFlags(t *testing.T) {
	requiredFlags := []struct {
		name string
		flag uintptr
	}{
		{"CLONE_NEWPID", unix.CLONE_NEWPID},
		{"CLONE_NEWUTS", unix.CLONE_NEWUTS},
		{"CLONE_NEWNS", unix.CLONE_NEWNS},
		{"CLONE_NEWNET", unix.CLONE_NEWNET},
	}

	for _, rf := range requiredFlags {
		if DefaultCloneFlags&rf.flag == 0 {
			t.Errorf("expected DefaultCloneFlags to contain %s, got flags: %x", rf.name, DefaultCloneFlags)
		}
	}
}
