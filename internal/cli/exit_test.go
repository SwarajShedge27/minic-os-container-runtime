package cli

import (
	"errors"
	"fmt"
	"syscall"
	"testing"
)

func TestExitCodeFromWaitStatus(t *testing.T) {
	testCases := []struct {
		name     string
		status   syscall.WaitStatus
		expected int
	}{
		{"ExitedZero", syscall.WaitStatus(0), 0},
		{"Exited42", syscall.WaitStatus(42 << 8), 42},
		{"Exited127", syscall.WaitStatus(127 << 8), 127},
		{"SignaledSIGINT", syscall.WaitStatus(int(syscall.SIGINT)), 130},
		{"SignaledSIGKILL", syscall.WaitStatus(int(syscall.SIGKILL)), 137},
		{"SignaledSIGSEGV", syscall.WaitStatus(int(syscall.SIGSEGV)), 139},
		{"SignaledSIGTERM", syscall.WaitStatus(int(syscall.SIGTERM)), 143},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := ExitCodeFromWaitStatus(tc.status)
			if actual != tc.expected {
				t.Errorf("expected exit code %d for %s, got %d", tc.expected, tc.name, actual)
			}
		})
	}
}

func TestExitCodeFromError(t *testing.T) {
	if code := ExitCodeFromError(nil); code != 0 {
		t.Errorf("expected 0 for nil error, got %d", code)
	}

	genericErr := errors.New("something went wrong")
	if code := ExitCodeFromError(genericErr); code != 1 {
		t.Errorf("expected 1 for generic error, got %d", code)
	}

	wrappedGeneric := fmt.Errorf("wrapped: %w", genericErr)
	if code := ExitCodeFromError(wrappedGeneric); code != 1 {
		t.Errorf("expected 1 for wrapped generic error, got %d", code)
	}
}
