package container

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/unix"
	"minic/internal/cgroup"
)

type Config struct {
	MemoryLimit string
	CPULimit    int
	PIDLimit    int
	Hostname    string
	Debug       bool
	RootFS      string
	Command     string
	Args        []string
}

type State string

const (
	Created  State = "Created"
	Starting State = "Starting"
	Running  State = "Running"
	Stopped  State = "Stopped"
)

type Manager struct {
	config *Config
	state  State
}

func NewManager(cfg *Config) *Manager {
	return &Manager{
		config: cfg,
		state:  Created,
	}
}

func (m *Manager) abort(cmd *exec.Cmd, w *os.File, err error) error {
	m.state = Stopped
	w.Close()
	cmd.Process.Kill()
	cmd.Wait()
	return err
}

func (m *Manager) Run() error {
	m.state = Starting
	if m.config.Debug {
		fmt.Println("[MiniC] Starting container")
	}

	selfPath := "/proc/self/exe"

	r, w, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("failed to create sync pipe: %w", err)
	}

	absRoot, err := filepath.Abs(m.config.RootFS)
	if err != nil {
		r.Close()
		w.Close()
		return fmt.Errorf("invalid rootfs path: %w", err)
	}

	args := append([]string{"child", "3", absRoot, m.config.Hostname, m.config.Command}, m.config.Args...)
	cmd := exec.Command(selfPath, args...)
	
	cmd.SysProcAttr = &unix.SysProcAttr{
		Cloneflags: unix.CLONE_NEWPID | unix.CLONE_NEWUTS | unix.CLONE_NEWNS,
	}

	cmd.Env = append(os.Environ(), fmt.Sprintf("MINIC_DEBUG=%v", m.config.Debug))
	cmd.ExtraFiles = []*os.File{r}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	m.state = Running
	
	if err := cmd.Start(); err != nil {
		m.state = Stopped
		r.Close()
		w.Close()
		return fmt.Errorf("failed to start child: %w", err)
	}
	r.Close()

	pid := cmd.Process.Pid
	if m.config.Debug {
		fmt.Printf("[MiniC] Child host PID: %d\n", pid)
		fmt.Println("[MiniC] PID namespace created")
		fmt.Println("[MiniC] UTS namespace created")
		fmt.Println("[MiniC] Mount namespace created")
		fmt.Println("[MiniC] Configuring cgroup")
	}

	cg, err := cgroup.NewManager(pid)
	if err != nil {
		return m.abort(cmd, w, fmt.Errorf("failed to configure cgroup: %w", err))
	}
	
	defer func() {
		if m.config.Debug {
			fmt.Println("[MiniC] Cleaning up")
		}
		if err := cg.Cleanup(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
	}()

	if err := cg.SetMemory(m.config.MemoryLimit); err != nil {
		return m.abort(cmd, w, fmt.Errorf("failed to configure cgroup: %w", err))
	}
	if m.config.Debug && m.config.MemoryLimit != "" {
		fmt.Printf("[MiniC] Memory limit: %s\n", m.config.MemoryLimit)
	}
	
	if err := cg.SetCPU(m.config.CPULimit); err != nil {
		return m.abort(cmd, w, fmt.Errorf("failed to configure cgroup: %w", err))
	}
	if m.config.Debug && m.config.CPULimit > 0 {
		fmt.Printf("[MiniC] CPU limit: %d%%\n", m.config.CPULimit)
	}
	
	if err := cg.SetPIDs(m.config.PIDLimit); err != nil {
		return m.abort(cmd, w, fmt.Errorf("failed to configure cgroup: %w", err))
	}
	if m.config.Debug && m.config.PIDLimit > 0 {
		fmt.Printf("[MiniC] PID limit: %d\n", m.config.PIDLimit)
	}

	if err := cg.AddProcess(pid); err != nil {
		return m.abort(cmd, w, fmt.Errorf("failed to add child to cgroup: %w", err))
	}
	
	if m.config.Debug {
		fmt.Println("[MiniC] Added process to cgroup")
		fmt.Println("[MiniC] Releasing child")
	}

	if _, err := w.Write([]byte{0}); err != nil {
		return m.abort(cmd, w, fmt.Errorf("failed to send sync signal: %w", err))
	}
	w.Close()
	
	err = cmd.Wait()
	m.state = Stopped
	if m.config.Debug {
		fmt.Println("[MiniC] Container stopped")
	}
	
	if err != nil {
		return fmt.Errorf("container process failed: %w", err)
	}
	
	return nil
}
