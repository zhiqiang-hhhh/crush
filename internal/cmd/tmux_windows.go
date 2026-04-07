//go:build windows

package cmd

import (
	"os"
	"os/exec"
)

// muxExec runs the mux binary and waits for it to exit.
// On Windows, syscall.Exec is not available, so we use os/exec
// and propagate the exit code.
func muxExec(bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			os.Exit(exit.ExitCode())
		}
		return err
	}
	os.Exit(0)
	return nil
}
