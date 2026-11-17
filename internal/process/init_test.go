package process

import (
	"strings"
	"testing"
)

func TestInitValidation(t *testing.T) {
	testCases := []struct {
		name        string
		syncFd      string
		uidStr      string
		gidStr      string
		errContains string
	}{
		{
			name:        "InvalidSyncFd",
			syncFd:      "invalid-fd",
			uidStr:      "0",
			gidStr:      "0",
			errContains: "invalid sync file descriptor",
		},
		{
			name:        "NegativeSyncFd",
			syncFd:      "-5",
			uidStr:      "0",
			gidStr:      "0",
			errContains: "invalid sync file descriptor",
		},
		{
			name:        "NonNumericUID",
			syncFd:      "3",
			uidStr:      "not-a-number",
			gidStr:      "0",
			errContains: "invalid UID",
		},
		{
			name:        "NegativeUID",
			syncFd:      "3",
			uidStr:      "-1",
			gidStr:      "0",
			errContains: "invalid UID",
		},
		{
			name:        "NonNumericGID",
			syncFd:      "3",
			uidStr:      "0",
			gidStr:      "not-a-number",
			errContains: "invalid GID",
		},
		{
			name:        "NegativeGID",
			syncFd:      "3",
			uidStr:      "0",
			gidStr:      "-1",
			errContains: "invalid GID",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := Init(tc.syncFd, "/tmp/m", "/tmp/l", "/tmp/u", "/tmp/w", "minic", tc.uidStr, tc.gidStr, "/bin/echo", nil)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.errContains)
			}
			if !strings.Contains(err.Error(), tc.errContains) {
				t.Errorf("expected error message containing %q, got: %v", tc.errContains, err)
			}
		})
	}
}
