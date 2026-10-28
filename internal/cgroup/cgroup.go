package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const cgroupRoot = "/sys/fs/cgroup"

// Manager handles configuring limits for a Linux cgroups v2 directory.
type Manager struct {
	path string
}

// NewManager verifies cgroups v2 availability and creates a dedicated child cgroup.
func NewManager(pid int) (*Manager, error) {
	// Verify cgroup v2 exists
	if _, err := os.Stat(cgroupRoot); os.IsNotExist(err) {
		return nil, fmt.Errorf("cgroups v2 not available at %s", cgroupRoot)
	}
	
	// Create minic-<pid>
	path := filepath.Join(cgroupRoot, fmt.Sprintf("minic-%d", pid))
	if err := os.Mkdir(path, 0755); err != nil && !os.IsExist(err) {
		return nil, fmt.Errorf("failed to create cgroup: %w", err)
	}

	return &Manager{path: path}, nil
}

// SetMemory writes to memory.max. Parses K, M, G suffixes.
func (m *Manager) SetMemory(limit string) error {
	if limit == "" {
		return nil
	}
	
	var bytes int64
	var err error
	
	upperLimit := strings.ToUpper(limit)
	if strings.HasSuffix(upperLimit, "M") {
		val, e := strconv.ParseInt(strings.TrimSuffix(upperLimit, "M"), 10, 64)
		if e != nil {
			return fmt.Errorf("invalid memory value: %w", e)
		}
		bytes = val * 1024 * 1024
	} else if strings.HasSuffix(upperLimit, "G") {
		val, e := strconv.ParseInt(strings.TrimSuffix(upperLimit, "G"), 10, 64)
		if e != nil {
			return fmt.Errorf("invalid memory value: %w", e)
		}
		bytes = val * 1024 * 1024 * 1024
	} else if strings.HasSuffix(upperLimit, "K") {
		val, e := strconv.ParseInt(strings.TrimSuffix(upperLimit, "K"), 10, 64)
		if e != nil {
			return fmt.Errorf("invalid memory value: %w", e)
		}
		bytes = val * 1024
	} else {
		bytes, err = strconv.ParseInt(limit, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid memory value: %w", err)
		}
	}

	if err := os.WriteFile(filepath.Join(m.path, "memory.max"), []byte(strconv.FormatInt(bytes, 10)), 0644); err != nil {
		return fmt.Errorf("failed to set memory limit: %w", err)
	}
	return nil
}

// SetCPU writes to cpu.max using a 100000 period.
func (m *Manager) SetCPU(limitPercentage int) error {
	if limitPercentage <= 0 {
		return nil
	}
	quota := int64(limitPercentage) * 1000
	val := fmt.Sprintf("%d 100000", quota)
	
	if err := os.WriteFile(filepath.Join(m.path, "cpu.max"), []byte(val), 0644); err != nil {
		return fmt.Errorf("failed to set cpu limit: %w", err)
	}
	return nil
}

// SetPIDs writes to pids.max.
func (m *Manager) SetPIDs(limit int) error {
	if limit <= 0 {
		return nil
	}
	
	if err := os.WriteFile(filepath.Join(m.path, "pids.max"), []byte(strconv.Itoa(limit)), 0644); err != nil {
		return fmt.Errorf("failed to set pids limit: %w", err)
	}
	return nil
}

// AddProcess adds a host PID to cgroup.procs.
func (m *Manager) AddProcess(pid int) error {
	if err := os.WriteFile(filepath.Join(m.path, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0644); err != nil {
		return fmt.Errorf("failed to add process to cgroup: %w", err)
	}
	return nil
}

// Cleanup removes the cgroup directory.
func (m *Manager) Cleanup() error {
	if err := os.Remove(m.path); err != nil {
		return fmt.Errorf("failed to remove cgroup %s: %w", m.path, err)
	}
	return nil
}
