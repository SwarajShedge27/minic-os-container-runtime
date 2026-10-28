package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const defaultCgroupRoot = "/sys/fs/cgroup"

// Manager handles configuring limits for a Linux cgroups v2 directory.
type Manager struct {
	root string
	path string
}

// NewManager verifies cgroups v2 availability at the default root and creates a dedicated child cgroup.
func NewManager(pid int) (*Manager, error) {
	return NewManagerWithPath(defaultCgroupRoot, pid)
}

// NewManagerWithPath allows injecting a custom root directory (useful for testing).
func NewManagerWithPath(root string, pid int) (*Manager, error) {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, fmt.Errorf("cgroups v2 not available at %s", root)
	}

	// Verify cgroups v2 unified hierarchy
	controllersPath := filepath.Join(root, "cgroup.controllers")
	if _, err := os.Stat(controllersPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("cgroups v2 unified hierarchy not detected at %s (missing cgroup.controllers)", root)
	}

	path := filepath.Join(root, fmt.Sprintf("minic-%d", pid))
	if err := os.Mkdir(path, 0755); err != nil && !os.IsExist(err) {
		return nil, fmt.Errorf("failed to create cgroup: %w", err)
	}

	return &Manager{root: root, path: path}, nil
}

// Path returns the path of the managed cgroup directory.
func (m *Manager) Path() string {
	return m.path
}

// SetMemory writes to memory.max. Parses K, M, G suffixes and "max".
func (m *Manager) SetMemory(limit string) error {
	if limit == "" {
		return nil
	}
	val, _, err := ParseMemoryLimit(limit)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(m.path, "memory.max"), []byte(val+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to set memory limit: %w", err)
	}
	return nil
}

// SetCPU writes to cpu.max using a 100000 period.
func (m *Manager) SetCPU(limitPercentage int) error {
	if limitPercentage <= 0 {
		return nil
	}
	val, err := CalculateCPUQuota(limitPercentage)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(m.path, "cpu.max"), []byte(val+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to set cpu limit: %w", err)
	}
	return nil
}

// SetPIDs writes to pids.max.
func (m *Manager) SetPIDs(limit int) error {
	if limit <= 0 {
		return nil
	}
	if err := ValidatePIDLimit(limit); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(m.path, "pids.max"), []byte(strconv.Itoa(limit)+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to set pids limit: %w", err)
	}
	return nil
}

// AddProcess adds a host PID to cgroup.procs.
func (m *Manager) AddProcess(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid PID: %d", pid)
	}
	if err := os.WriteFile(filepath.Join(m.path, "cgroup.procs"), []byte(strconv.Itoa(pid)+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to add process to cgroup: %w", err)
	}
	return nil
}

// Cleanup removes the cgroup directory using a bounded, safe retry strategy.
func (m *Manager) Cleanup() error {
	if m.path == "" {
		return nil
	}

	cleanPath := filepath.Clean(m.path)
	cleanRoot := filepath.Clean(m.root)

	// Safety check: ensure m.path is strictly a subpath of m.root
	if cleanPath == cleanRoot || !strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator)) {
		return fmt.Errorf("refusing to remove unsafe cgroup path: %s", m.path)
	}

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		// Attempt termination of any lingering processes
		m.killProcesses()

		// In mock filesystems (such as unit tests), regular files can be removed
		// (on real cgroupfs these are virtual kernel files and cannot be unlinked).
		entries, _ := os.ReadDir(cleanPath)
		for _, entry := range entries {
			if !entry.IsDir() {
				_ = os.Remove(filepath.Join(cleanPath, entry.Name()))
			}
		}

		err := os.Remove(cleanPath)
		if err == nil || os.IsNotExist(err) {
			return nil
		}
		lastErr = err

		time.Sleep(20 * time.Millisecond)
	}

	return fmt.Errorf("failed to remove cgroup %s: %w", m.path, lastErr)
}

func (m *Manager) killProcesses() {
	// If cgroup.kill exists (cgroup v2 on Linux 5.14+), trigger it.
	// We open without O_CREATE so we never create a regular file if the kernel file is absent.
	if f, err := os.OpenFile(filepath.Join(m.path, "cgroup.kill"), os.O_WRONLY, 0); err == nil {
		_, _ = f.Write([]byte("1\n"))
		_ = f.Close()
	}

	// Inspect cgroup.procs for any lingering processes in this cgroup
	data, err := os.ReadFile(filepath.Join(m.path, "cgroup.procs"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if pid, err := strconv.Atoi(trimmed); err == nil && pid > 1 {
			_ = unix.Kill(pid, unix.SIGKILL)
		}
	}
}
