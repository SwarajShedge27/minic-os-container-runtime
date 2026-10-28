package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"minic/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
