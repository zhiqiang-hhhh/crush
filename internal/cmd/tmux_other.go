//go:build !windows

package cmd

import "syscall"

// muxExec replaces the current process with the mux binary.
// On Unix, this never returns on success.
func muxExec(bin string, args []string) error {
	argv := append([]string{bin}, args...)
	return syscall.Exec(bin, argv, syscall.Environ())
}
