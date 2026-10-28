package security

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestGetLastCap(t *testing.T) {
	lastCap := GetLastCap()
	if lastCap < 0 || lastCap >= 64 {
		t.Fatalf("expected last capability in range [0, 63], got: %d", lastCap)
	}

	// On Linux 3.8+ cap_last_cap is at least CAP_BLOCK_SUSPEND (36)
	if lastCap < 36 {
		t.Logf("warning: unexpectedly low cap_last_cap: %d", lastCap)
	}
}

func TestClearAmbientCapabilities(t *testing.T) {
	// Clearing ambient capabilities succeeds for unprivileged callers or returns EINVAL on pre-4.3 kernels
	if err := ClearAmbientCapabilities(); err != nil {
		t.Fatalf("ClearAmbientCapabilities failed: %v", err)
	}
}

func TestEnableNoNewPrivsInSubprocess(t *testing.T) {
	// Since no_new_privs cannot be undone on the current process, test in a subprocess
	if os.Getenv("TEST_SUBPROCESS_NO_NEW_PRIVS") == "1" {
		if err := EnableNoNewPrivs(); err != nil {
			t.Fatalf("EnableNoNewPrivs failed: %v", err)
		}
		res, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
		if err != nil {
			t.Fatalf("PR_GET_NO_NEW_PRIVS failed: %v", err)
		}
		if res != 1 {
			t.Fatalf("expected PR_GET_NO_NEW_PRIVS == 1, got: %d", res)
		}
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestEnableNoNewPrivsInSubprocess")
	cmd.Env = append(os.Environ(), "TEST_SUBPROCESS_NO_NEW_PRIVS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess failed: %v\nOutput: %s", err, string(out))
	}
}

func TestPrivilegedSecurityHardening(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("skipping privileged security hardening test: requires root (run with sudo)")
	}

	// Run in a subprocess so we don't drop capabilities for the entire test runner
	if os.Getenv("TEST_SUBPROCESS_APPLY_SECURITY") == "1" {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		if err := ApplyPrivilegeRestrictions(false); err != nil {
			t.Fatalf("ApplyPrivilegeRestrictions failed: %v", err)
		}

		statusData, err := os.ReadFile("/proc/self/status")
		if err != nil {
			t.Fatalf("failed to read /proc/self/status: %v", err)
		}

		lines := strings.Split(string(statusData), "\n")
		caps := make(map[string]string)
		for _, line := range lines {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				caps[key] = val
			}
		}

		// Verify NoNewPrivs
		if caps["NoNewPrivs"] != "1" {
			t.Errorf("expected NoNewPrivs: 1, got: %q", caps["NoNewPrivs"])
		}

		// Verify zeroed capability sets
		for _, capKey := range []string{"CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb"} {
			val, exists := caps[capKey]
			if !exists {
				continue
			}
			trimmedVal := strings.TrimLeft(val, "0")
			if trimmedVal != "" {
				t.Errorf("expected %s to be all zeros, got: %q", capKey, val)
			}
		}
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestPrivilegedSecurityHardening")
	cmd.Env = append(os.Environ(), "TEST_SUBPROCESS_APPLY_SECURITY=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess failed: %v\nOutput: %s", err, string(out))
	}
}
