package cli

import (
	"errors"
	"os/exec"
	"syscall"
)

// ExitCodeFromWaitStatus converts a syscall.WaitStatus into an exit code.
// For processes exited normally, it returns the exit status code.
// For processes terminated by a signal N, it returns 128 + N.
func ExitCodeFromWaitStatus(ws syscall.WaitStatus) int {
	if ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	if ws.Exited() {
		return ws.ExitStatus()
	}
	return 1
}

// ExitCodeFromError extracts a standard exit code from an error.
// If the error wraps an *exec.ExitError, it inspects the underlying wait status.
// If the exit code cannot be determined or is negative, it returns 1.
func ExitCodeFromError(err error) int {
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return ExitCodeFromWaitStatus(ws)
		}
		if code := exitErr.ExitCode(); code >= 0 {
			return code
		}
	}

	return 1
}
