package cgroup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCgroupDetection(t *testing.T) {
	tempDir := t.TempDir()

	// Case 1: non-existent directory
	_, err := NewManagerWithPath(filepath.Join(tempDir, "nonexistent"), 1234)
	if err == nil {
		t.Fatal("expected error for nonexistent cgroup root")
	}

	// Case 2: directory exists, but missing cgroup.controllers
	mockV1Dir := filepath.Join(tempDir, "v1_root")
	if err := os.MkdirAll(mockV1Dir, 0755); err != nil {
		t.Fatal(err)
	}
	_, err = NewManagerWithPath(mockV1Dir, 1234)
	if err == nil {
		t.Fatal("expected error when cgroup.controllers is missing")
	}
	if !strings.Contains(err.Error(), "missing cgroup.controllers") {
		t.Errorf("expected error mentioning cgroup.controllers, got: %v", err)
	}

	// Case 3: valid cgroup v2 mock root with cgroup.controllers
	mockV2Dir := filepath.Join(tempDir, "v2_root")
	if err := os.MkdirAll(mockV2Dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mockV2Dir, "cgroup.controllers"), []byte("cpu memory pids\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr, err := NewManagerWithPath(mockV2Dir, 4321)
	if err != nil {
		t.Fatalf("unexpected error creating manager: %v", err)
	}
	expectedPath := filepath.Join(mockV2Dir, "minic-4321")
	if mgr.Path() != expectedPath {
		t.Errorf("expected path %q, got %q", expectedPath, mgr.Path())
	}
	if _, err := os.Stat(expectedPath); err != nil {
		t.Errorf("expected cgroup dir %s to exist on disk: %v", expectedPath, err)
	}
}

func TestCgroupFileWritesAndCleanup(t *testing.T) {
	tempDir := t.TempDir()
	mockRoot := filepath.Join(tempDir, "cgroup2")
	if err := os.MkdirAll(mockRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mockRoot, "cgroup.controllers"), []byte("cpu memory pids\n"), 0644); err != nil {
		t.Fatal(err)
	}

	pid := 5678
	mgr, err := NewManagerWithPath(mockRoot, pid)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Test Memory write
	if err := mgr.SetMemory("100M"); err != nil {
		t.Errorf("SetMemory failed: %v", err)
	}
	memData, err := os.ReadFile(filepath.Join(mgr.Path(), "memory.max"))
	if err != nil || strings.TrimSpace(string(memData)) != "104857600" {
		t.Errorf("expected memory.max=104857600, got %q (err=%v)", string(memData), err)
	}

	// Test Memory "max" write
	if err := mgr.SetMemory("max"); err != nil {
		t.Errorf("SetMemory max failed: %v", err)
	}
	memData, err = os.ReadFile(filepath.Join(mgr.Path(), "memory.max"))
	if err != nil || strings.TrimSpace(string(memData)) != "max" {
		t.Errorf("expected memory.max=max, got %q (err=%v)", string(memData), err)
	}

	// Test CPU write
	if err := mgr.SetCPU(50); err != nil {
		t.Errorf("SetCPU failed: %v", err)
	}
	cpuData, err := os.ReadFile(filepath.Join(mgr.Path(), "cpu.max"))
	if err != nil || strings.TrimSpace(string(cpuData)) != "50000 100000" {
		t.Errorf("expected cpu.max='50000 100000', got %q (err=%v)", string(cpuData), err)
	}

	// Test PIDs write
	if err := mgr.SetPIDs(25); err != nil {
		t.Errorf("SetPIDs failed: %v", err)
	}
	pidsData, err := os.ReadFile(filepath.Join(mgr.Path(), "pids.max"))
	if err != nil || strings.TrimSpace(string(pidsData)) != "25" {
		t.Errorf("expected pids.max=25, got %q (err=%v)", string(pidsData), err)
	}

	// Test AddProcess write
	if err := mgr.AddProcess(pid); err != nil {
		t.Errorf("AddProcess failed: %v", err)
	}
	procsData, err := os.ReadFile(filepath.Join(mgr.Path(), "cgroup.procs"))
	if err != nil || strings.TrimSpace(string(procsData)) != "5678" {
		t.Errorf("expected cgroup.procs=5678, got %q (err=%v)", string(procsData), err)
	}

	// Test Safe Cleanup
	if err := mgr.Cleanup(); err != nil {
		t.Errorf("Cleanup failed: %v", err)
	}
	if _, err := os.Stat(mgr.Path()); !os.IsNotExist(err) {
		t.Errorf("expected cgroup dir %s to be removed, stat err: %v", mgr.Path(), err)
	}
}

func TestCgroupUnsafePathRejection(t *testing.T) {
	tempDir := t.TempDir()

	// Attempt to clean up root directory itself
	mgr := &Manager{root: tempDir, path: tempDir}
	err := mgr.Cleanup()
	if err == nil {
		t.Fatal("expected error when attempting to clean up root directory")
	}
	if !strings.Contains(err.Error(), "refusing to remove unsafe cgroup path") {
		t.Errorf("expected safety error message, got: %v", err)
	}

	// Attempt to clean up an outside path
	outsidePath := filepath.Join(t.TempDir(), "outside")
	mgrOutside := &Manager{root: tempDir, path: outsidePath}
	err = mgrOutside.Cleanup()
	if err == nil {
		t.Fatal("expected error when attempting to clean up outside path")
	}
	if !strings.Contains(err.Error(), "refusing to remove unsafe cgroup path") {
		t.Errorf("expected safety error message, got: %v", err)
	}
}
