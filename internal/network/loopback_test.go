package network

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestValidation(t *testing.T) {
	// Empty interface name
	if err := ConfigureInterface(""); err == nil {
		t.Fatal("expected error for empty interface name, got nil")
	}

	if _, err := GetInterfaceFlags(""); err == nil {
		t.Fatal("expected error for empty interface name in GetInterfaceFlags, got nil")
	}

	// Excessively long interface name exceeding IFNAMSIZ (16 bytes)
	longName := strings.Repeat("a", 32)
	if err := ConfigureInterface(longName); err == nil {
		t.Fatal("expected error for interface name exceeding IFNAMSIZ, got nil")
	}

	// Non-existent interface name
	if err := ConfigureInterface("nonexistent99"); err == nil {
		t.Fatal("expected error for non-existent interface, got nil")
	}
}

func TestGetInterfaceFlagsLoopback(t *testing.T) {
	// On Linux, querying flags for the existing loopback interface "lo" succeeds without root.
	flags, err := GetInterfaceFlags(LoopbackInterface)
	if err != nil {
		t.Skipf("skipping interface query (socket may be restricted in environment): %v", err)
	}

	// Host loopback typically has IFF_UP and IFF_LOOPBACK set
	if flags&uint16(unix.IFF_LOOPBACK) == 0 {
		t.Errorf("expected IFF_LOOPBACK flag on %s, got flags: %x", LoopbackInterface, flags)
	}
}

func TestPrivilegedLoopbackConfiguration(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("skipping privileged loopback test: requires root (run with sudo)")
	}

	// Lock OS thread before unsharing network namespace
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Unshare network namespace for this thread to test loopback configuration in isolation
	if err := unix.Unshare(unix.CLONE_NEWNET); err != nil {
		t.Skipf("cannot unshare network namespace in this environment: %v", err)
	}

	// In a newly unshared network namespace, loopback starts with IFF_UP cleared
	preFlags, err := GetInterfaceFlags(LoopbackInterface)
	if err != nil {
		t.Fatalf("failed to query loopback flags in new netns: %v", err)
	}
	if preFlags&uint16(unix.IFF_UP) != 0 {
		t.Logf("note: loopback was already UP in new netns (flags: %x)", preFlags)
	}

	// Configure loopback interface UP
	if err := SetupLoopback(false); err != nil {
		t.Fatalf("SetupLoopback failed in isolated netns: %v", err)
	}

	// Verify loopback interface is now UP and RUNNING
	postFlags, err := GetInterfaceFlags(LoopbackInterface)
	if err != nil {
		t.Fatalf("failed to query loopback flags after SetupLoopback: %v", err)
	}

	if postFlags&uint16(unix.IFF_UP) == 0 {
		t.Errorf("expected IFF_UP on %s after SetupLoopback, got flags: %x", LoopbackInterface, postFlags)
	}
	if postFlags&uint16(unix.IFF_RUNNING) == 0 {
		t.Errorf("expected IFF_RUNNING on %s after SetupLoopback, got flags: %x", LoopbackInterface, postFlags)
	}
}
