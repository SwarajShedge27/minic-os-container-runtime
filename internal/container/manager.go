package container

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"minic/internal/cgroup"
	"minic/internal/filesystem"
)

// DefaultCloneFlags defines the Linux namespace clone flags used for container isolation.
// MiniC isolates PID, UTS, Mount, and Network namespaces.
const DefaultCloneFlags = unix.CLONE_NEWPID | unix.CLONE_NEWUTS | unix.CLONE_NEWNS | unix.CLONE_NEWNET

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

// State returns the current container manager lifecycle state.
func (m *Manager) State() State {
	return m.state
}

func (m *Manager) abort(cmd *exec.Cmd, w *os.File, err error) error {
	m.state = Stopped
	if w != nil {
		w.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	return err
}

func (m *Manager) Run() error {
	m.state = Starting
	if m.config.Debug {
		fmt.Println("[MiniC] Starting container")
	}

	// 1. Stage the rootfs copy onto a Linux-native filesystem with OverlayFS directories
	if m.config.Debug {
		fmt.Println("[MiniC] Staging rootfs into temporary OverlayFS directory")
	}
	staging, err := filesystem.StageRootfs(m.config.RootFS)
	if err != nil {
		m.state = Stopped
		return fmt.Errorf("failed to stage rootfs: %w", err)
	}

	// Staging cleanup deferred first so it executes LAST among defers (after process termination & cgroup cleanup)
	defer func() {
		if m.config.Debug {
			fmt.Println("[MiniC] Cleaning up temporary filesystem staging")
		}
		if err := staging.Cleanup(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
	}()

	selfPath := "/proc/self/exe"

	r, w, err := os.Pipe()
	if err != nil {
		m.state = Stopped
		return fmt.Errorf("failed to create sync pipe: %w", err)
	}

	args := append([]string{
		"child",
		"3",
		staging.Merged,
		staging.Lower,
		staging.Upper,
		staging.Work,
		m.config.Hostname,
		m.config.Command,
	}, m.config.Args...)
	cmd := exec.Command(selfPath, args...)

	cmd.SysProcAttr = &unix.SysProcAttr{
		Cloneflags: DefaultCloneFlags,
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
		fmt.Println("[MiniC] Network namespace created")
		fmt.Println("[MiniC] Configuring cgroup")
	}

	cg, err := cgroup.NewManager(pid)
	if err != nil {
		return m.abort(cmd, w, fmt.Errorf("failed to configure cgroup: %w", err))
	}

	// Cgroup cleanup runs BEFORE staging cleanup (LIFO)
	defer func() {
		if m.config.Debug {
			fmt.Println("[MiniC] Cleaning up cgroup")
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

	// Set up signal notifications for SIGINT and SIGTERM
	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	var waitErr error
	select {
	case waitErr = <-waitDone:
		// Process finished normally without caught parent signals.
	case sig := <-sigChan:
		if m.config.Debug {
			fmt.Printf("[MiniC] Caught signal %v, forwarding to child\n", sig)
		}
		if cmd.Process != nil {
			_ = cmd.Process.Signal(sig)
		}

		// Wait up to 5 seconds for child to terminate before escalating to SIGKILL.
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()

	waitLoop:
		for {
			select {
			case waitErr = <-waitDone:
				break waitLoop
			case sig2 := <-sigChan:
				if cmd.Process != nil {
					_ = cmd.Process.Signal(sig2)
				}
			case <-timer.C:
				if m.config.Debug {
					fmt.Println("[MiniC] Grace period expired, sending SIGKILL to container")
				}
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
			}
		}
	}

	m.state = Stopped
	if m.config.Debug {
		fmt.Println("[MiniC] Container stopped")
	}

	if waitErr != nil {
		return fmt.Errorf("container process failed: %w", waitErr)
	}

	return nil
}
