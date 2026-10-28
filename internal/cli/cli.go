package cli

import (
	"flag"
	"fmt"
	"os"

	"minic/internal/container"
	"minic/internal/process"
)

func Run(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("expected a command, e.g., 'run'")
	}

	command := args[0]
	switch command {
	case "run":
		return handleRun(args[1:])
	case "child":
		return handleChild(args[1:])
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func handleChild(args []string) error {
	if len(args) < 4 {
		return fmt.Errorf("missing arguments in child")
	}
	return process.Init(args[0], args[1], args[2], args[3], args[4:])
}

func handleRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)

	memory := fs.String("memory", "", "Memory limit (e.g., 100M)")
	cpu := fs.Int("cpu", 0, "CPU limit (percentage, e.g., 50)")
	pids := fs.Int("pids", 0, "PID limit (e.g., 50)")
	hostname := fs.String("hostname", "minic", "Hostname for the container")
	debug := fs.Bool("debug", false, "Enable diagnostic debug mode")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *cpu < 0 {
		return fmt.Errorf("invalid CPU value: cannot be negative")
	}
	if *pids < 0 {
		return fmt.Errorf("invalid PID limit: cannot be negative")
	}

	positionalArgs := fs.Args()
	if len(positionalArgs) < 2 {
		return fmt.Errorf("usage: minic run [options] <rootfs> <command> [args...]")
	}
	
	if _, err := os.Stat(positionalArgs[0]); os.IsNotExist(err) {
		return fmt.Errorf("nonexistent rootfs: %s", positionalArgs[0])
	}

	cfg := &container.Config{
		MemoryLimit: *memory,
		CPULimit:    *cpu,
		PIDLimit:    *pids,
		Hostname:    *hostname,
		Debug:       *debug,
		RootFS:      positionalArgs[0],
		Command:     positionalArgs[1],
		Args:        positionalArgs[2:],
	}

	mgr := container.NewManager(cfg)
	return mgr.Run()
}
