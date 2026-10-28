package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpCommands(t *testing.T) {
	testCases := []struct {
		name string
		args []string
	}{
		{"TopLevelHelpFlag", []string{"--help"}},
		{"TopLevelShortHelpFlag", []string{"-h"}},
		{"TopLevelHelpCommand", []string{"help"}},
		{"RunHelpFlag", []string{"run", "--help"}},
		{"RunShortHelpFlag", []string{"run", "-h"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := RunWithIO(tc.args, &stdout, &stderr)
			if err != nil {
				t.Fatalf("expected nil error for help request, got: %v", err)
			}
			outStr := stdout.String()
			if !strings.Contains(outStr, "Usage:") && !strings.Contains(outStr, "usage:") {
				t.Errorf("expected usage output, got: %q", outStr)
			}
			if stderr.Len() > 0 {
				t.Errorf("expected empty stderr for help, got: %q", stderr.String())
			}
		})
	}
}

func TestValidationErrors(t *testing.T) {
	testCases := []struct {
		name        string
		args        []string
		errContains string
	}{
		{
			name:        "EmptyArgs",
			args:        []string{},
			errContains: "expected a command",
		},
		{
			name:        "UnknownCommand",
			args:        []string{"foo"},
			errContains: "unknown command",
		},
		{
			name:        "RunMissingAllArgs",
			args:        []string{"run"},
			errContains: "usage: minic run",
		},
		{
			name:        "RunMissingCommandArg",
			args:        []string{"run", "./rootfs"},
			errContains: "usage: minic run",
		},
		{
			name:        "RunNonexistentRootfs",
			args:        []string{"run", "/path/does/not/exist/minic_test_xyz", "/bin/echo"},
			errContains: "nonexistent rootfs",
		},
		{
			name:        "NegativeCPU",
			args:        []string{"run", "-cpu", "-10", "./rootfs", "/bin/echo"},
			errContains: "invalid CPU value",
		},
		{
			name:        "ZeroCPU",
			args:        []string{"run", "-cpu", "0", "./rootfs", "/bin/echo"},
			errContains: "invalid CPU value",
		},
		{
			name:        "NegativePIDs",
			args:        []string{"run", "-pids", "-5", "./rootfs", "/bin/echo"},
			errContains: "invalid PID limit",
		},
		{
			name:        "ZeroPIDs",
			args:        []string{"run", "-pids", "0", "./rootfs", "/bin/echo"},
			errContains: "invalid PID limit",
		},
		{
			name:        "InvalidMemoryString",
			args:        []string{"run", "-memory", "100MB", "./rootfs", "/bin/echo"},
			errContains: "invalid unit suffix",
		},
		{
			name:        "ZeroMemory",
			args:        []string{"run", "-memory", "0", "./rootfs", "/bin/echo"},
			errContains: "must be greater than zero",
		},
		{
			name:        "NegativeMemory",
			args:        []string{"run", "-memory", "-10M", "./rootfs", "/bin/echo"},
			errContains: "cannot be negative",
		},
		{
			name:        "ChildMissingArgs",
			args:        []string{"child", "1", "2"},
			errContains: "missing arguments in child",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := RunWithIO(tc.args, &stdout, &stderr)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.errContains)
			}
			if !strings.Contains(err.Error(), tc.errContains) {
				t.Errorf("expected error containing %q, got %q", tc.errContains, err.Error())
			}
		})
	}
}
