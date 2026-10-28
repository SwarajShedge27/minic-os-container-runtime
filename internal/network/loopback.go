package network

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// LoopbackInterface is the standard name of the Linux loopback interface.
const LoopbackInterface = "lo"

// ConfigureInterface brings up a network interface by name in the current network namespace.
// It retrieves current interface flags using SIOCGIFFLAGS and sets IFF_UP and IFF_RUNNING
// using SIOCSIFFLAGS while preserving all other interface flags.
func ConfigureInterface(ifName string) error {
	if ifName == "" {
		return fmt.Errorf("interface name cannot be empty")
	}

	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("failed to open socket for network configuration: %w", err)
	}
	defer unix.Close(fd)

	ifr, err := unix.NewIfreq(ifName)
	if err != nil {
		return fmt.Errorf("invalid interface name %q: %w", ifName, err)
	}

	// 1. Retrieve current interface flags
	if err := unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, ifr); err != nil {
		return fmt.Errorf("failed to get flags for interface %q: %w", ifName, err)
	}

	// 2. Add IFF_UP and IFF_RUNNING to current flags
	currentFlags := ifr.Uint16()
	newFlags := currentFlags | uint16(unix.IFF_UP|unix.IFF_RUNNING)
	ifr.SetUint16(newFlags)

	// 3. Set updated flags
	if err := unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, ifr); err != nil {
		return fmt.Errorf("failed to set flags for interface %q: %w", ifName, err)
	}

	return nil
}

// GetInterfaceFlags retrieves the current interface flags for the specified interface.
func GetInterfaceFlags(ifName string) (uint16, error) {
	if ifName == "" {
		return 0, fmt.Errorf("interface name cannot be empty")
	}

	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to open socket for interface query: %w", err)
	}
	defer unix.Close(fd)

	ifr, err := unix.NewIfreq(ifName)
	if err != nil {
		return 0, fmt.Errorf("invalid interface name %q: %w", ifName, err)
	}

	if err := unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, ifr); err != nil {
		return 0, fmt.Errorf("failed to get flags for interface %q: %w", ifName, err)
	}

	return ifr.Uint16(), nil
}

// SetupLoopback brings up the container's isolated loopback interface.
func SetupLoopback(isDebug bool) error {
	if err := ConfigureInterface(LoopbackInterface); err != nil {
		return fmt.Errorf("failed to bring up loopback interface: %w", err)
	}

	if isDebug {
		fmt.Printf("[MiniC] Loopback interface (%s) is UP\n", LoopbackInterface)
	}

	return nil
}
