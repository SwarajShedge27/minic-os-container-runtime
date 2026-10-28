package security

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// DefaultLastCap is a fallback value if neither /proc nor PR_CAPBSET_READ are available.
const DefaultLastCap = 40

// GetLastCap dynamically determines the highest valid capability index for the running kernel.
func GetLastCap() int {
	data, err := os.ReadFile("/proc/sys/kernel/cap_last_cap")
	if err == nil {
		str := strings.TrimSpace(string(data))
		if val, err := strconv.Atoi(str); err == nil && val >= 0 && val < 64 {
			return val
		}
	}

	// Fallback probe using PR_CAPBSET_READ
	last := 0
	for c := 0; c < 64; c++ {
		res, err := unix.PrctlRetInt(unix.PR_CAPBSET_READ, uintptr(c), 0, 0, 0)
		if err != nil || res < 0 {
			break
		}
		last = c
	}
	if last > 0 {
		return last
	}
	return DefaultLastCap
}

// DropBoundingSet removes all capabilities from the process's bounding set.
// This requires CAP_SETPCAP to still be present in the effective/permitted sets.
func DropBoundingSet(lastCap int) error {
	for c := 0; c <= lastCap; c++ {
		inSet, err := unix.PrctlRetInt(unix.PR_CAPBSET_READ, uintptr(c), 0, 0, 0)
		if err != nil {
			// If reading this capability index fails, we've reached the end of valid capabilities
			break
		}
		if inSet == 1 {
			if err := unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(c), 0, 0, 0); err != nil {
				return fmt.Errorf("failed to drop capability %d from bounding set: %w", c, err)
			}
		}
	}
	return nil
}

// ClearAmbientCapabilities resets all ambient capabilities if supported by the kernel.
func ClearAmbientCapabilities() error {
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); err != nil {
		// Ignore EINVAL on kernels prior to 4.3 that do not support ambient capabilities
		if err == unix.EINVAL {
			return nil
		}
		return fmt.Errorf("failed to clear ambient capabilities: %w", err)
	}
	return nil
}

// ClearCapabilitySets clears Effective, Permitted, and Inheritable capability sets.
func ClearCapabilitySets() error {
	hdr := unix.CapUserHeader{
		Version: unix.LINUX_CAPABILITY_VERSION_3,
		Pid:     0, // 0 targets the calling thread
	}
	var data [2]unix.CapUserData // Zeroed data clears Effective, Permitted, and Inheritable

	if err := unix.Capset(&hdr, &data[0]); err != nil {
		return fmt.Errorf("failed to clear capability sets via capset: %w", err)
	}
	return nil
}

// EnableNoNewPrivs sets the PR_SET_NO_NEW_PRIVS flag to prevent the calling thread
// and its children from gaining privileges through execve transitions.
func EnableNoNewPrivs() error {
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("failed to set PR_SET_NO_NEW_PRIVS: %w", err)
	}
	return nil
}

// ApplyPrivilegeRestrictions applies full security hardening to the calling thread:
// 1. Drops all capabilities from the Bounding set.
// 2. Clears all Ambient capabilities.
// 3. Enables PR_SET_NO_NEW_PRIVS.
// 4. Clears Effective, Permitted, and Inheritable capability sets.
func ApplyPrivilegeRestrictions(isDebug bool) error {
	lastCap := GetLastCap()

	// 1. Drop Bounding Set while process still holds CAP_SETPCAP
	if err := DropBoundingSet(lastCap); err != nil {
		return fmt.Errorf("security hardening: %w", err)
	}

	// 2. Clear Ambient Capabilities
	if err := ClearAmbientCapabilities(); err != nil {
		return fmt.Errorf("security hardening: %w", err)
	}

	// 3. Enable PR_SET_NO_NEW_PRIVS
	if err := EnableNoNewPrivs(); err != nil {
		return fmt.Errorf("security hardening: %w", err)
	}

	// 4. Clear Effective, Permitted, Inheritable sets
	if err := ClearCapabilitySets(); err != nil {
		return fmt.Errorf("security hardening: %w", err)
	}

	if isDebug {
		fmt.Println("[MiniC] Dropped all capabilities (Bnd, Amb, Eff, Prm, Inh) and enabled no_new_privs")
	}

	return nil
}
