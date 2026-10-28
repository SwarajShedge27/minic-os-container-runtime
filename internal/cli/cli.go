package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"minic/internal/cgroup"
	"minic/internal/container"
	"minic/internal/process"
)

// Run parses command line arguments using os.Stdout and os.Stderr.
func Run(args []string) error {
	return RunWithIO(args, os.Stdout, os.Stderr)
}

// RunWithIO parses arguments with customizable stdout and stderr writers.
func RunWithIO(args []string, stdout, stderr io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("expected a command, e.g., 'run'")
	}

	command := args[0]
	switch command {
	case "help", "--help", "-h":
		printTopLevelUsage(stdout)
		return nil
	case "run":
		return handleRun(args[1:], stdout, stderr)
	case "child":
		return handleChild(args[1:])
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func printTopLevelUsage(out io.Writer) {
	fmt.Fprintln(out, "MiniC — Lightweight Linux Container Runtime from Scratch")
	fmt.Fprintln(out, "\nUsage:")
	fmt.Fprintln(out, "  minic <command> [arguments]")
	fmt.Fprintln(out, "\nAvailable Commands:")
	fmt.Fprintln(out, "  run        Run a command in a new isolated container")
	fmt.Fprintln(out, "  help       Show help for minic")
	fmt.Fprintln(out, "\nRun 'minic run --help' for details on container options.")
}

func handleChild(args []string) error {
	if len(args) < 7 {
		return fmt.Errorf("missing arguments in child")
	}
	return process.Init(args[0], args[1], args[2], args[3], args[4], args[5], args[6], args[7:])
}

func handleRun(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stdout)

	fs.Usage = func() {
		fmt.Fprintln(stdout, "Usage: minic run [options] <rootfs> <command> [args...]")
		fmt.Fprintln(stdout, "\nOptions:")
		fs.PrintDefaults()
	}

	memory := fs.String("memory", "", "Memory limit (e.g., 100M, 1G, max)")
	cpu := fs.Int("cpu", 0, "CPU limit (percentage, e.g., 50 for 50%, 150 for 1.5 CPUs)")
	pids := fs.Int("pids", 0, "PID limit (e.g., 50)")
	hostname := fs.String("hostname", "minic", "Hostname for the container")
	debug := fs.Bool("debug", false, "Enable diagnostic debug mode")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	var cpuSet, pidsSet bool
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "cpu" {
			cpuSet = true
		}
		if f.Name == "pids" {
			pidsSet = true
		}
	})

	if cpuSet && *cpu <= 0 {
		return fmt.Errorf("invalid CPU value: must be greater than zero")
	}
	if pidsSet && *pids <= 0 {
		return fmt.Errorf("invalid PID limit: must be greater than zero")
	}

	if *memory != "" {
		if _, _, err := cgroup.ParseMemoryLimit(*memory); err != nil {
			return err
		}
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
