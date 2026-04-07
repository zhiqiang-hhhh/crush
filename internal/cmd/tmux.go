package cmd

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/spf13/cobra"
)

//go:embed tmux.conf
var embeddedTmuxConf string

// tmuxSocketPath returns the path to the dedicated crush tmux socket.
func tmuxSocketPath() string {
	dir := os.TempDir()
	return filepath.Join(dir, "tmux-crush")
}

// tmuxConfPath returns the path where the embedded tmux config is written.
func tmuxConfPath() string {
	return filepath.Join(filepath.Dir(config.GlobalConfig()), "tmux.conf")
}

// ensureTmuxConf writes the embedded tmux config to disk only if the
// file does not already exist. This allows users to customize the file
// without it being overwritten on upgrade.
func ensureTmuxConf() (string, error) {
	path := tmuxConfPath()
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(embeddedTmuxConf), 0o644)
}

// findMux returns the path to tmux or psmux if available.
func findMux() string {
	for _, name := range []string{"tmux", "psmux"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// shouldAutoTmux returns true if crush should exec into a tmux session.
// Conditions: not already in tmux, tmux/psmux available, not disabled.
func shouldAutoTmux() bool {
	if os.Getenv("TMUX") != "" {
		return false
	}
	if os.Getenv("CRUSH_NO_TMUX") != "" {
		return false
	}
	if runtime.GOOS == "windows" {
		return findMux() != ""
	}
	return findMux() != ""
}

// execIntoTmux replaces the current process with a tmux session running crush.
// On Unix this uses syscall.Exec; on Windows it uses os/exec and waits.
//
// This mirrors the behavior of startcrush-tmux:
//
//	TMUX=/tmp/tmux-crush tmux -f <config> -u new-session -A -s crush -c <cwd> <crush> --continue
func execIntoTmux(crushArgs []string) error {
	muxBin := findMux()
	if muxBin == "" {
		return nil
	}

	confPath, err := ensureTmuxConf()
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	// Set dedicated socket path so crush's tmux doesn't interfere with
	// the user's regular tmux sessions.
	socket := tmuxSocketPath()

	// Build tmux args:
	// tmux -S <socket> -f <config> -u new-session -A -s crush -c <cwd> <exe> [args...]
	args := []string{
		"-S", socket,
		"-f", confPath,
		"-u",
		"new-session", "-A",
		"-s", "crush",
		"-c", cwd,
	}

	// Append the crush executable and its original args as the tmux
	// window command.
	windowCmd := append([]string{exe}, crushArgs...)
	args = append(args, windowCmd...)

	return muxExec(muxBin, args)
}

// buildInnerTmuxArgs builds the argument list for the crush process that
// will run inside tmux. It preserves the user's original flags and defaults
// to --continue if no session flag was given.
func buildInnerTmuxArgs(cmd *cobra.Command) []string {
	var args []string

	yolo, _ := cmd.Flags().GetBool("yolo")
	if yolo {
		args = append(args, "--yolo")
	}

	cwd, _ := cmd.Flags().GetString("cwd")
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}

	dataDir, _ := cmd.Flags().GetString("data-dir")
	if dataDir != "" {
		args = append(args, "--data-dir", dataDir)
	}

	debug, _ := cmd.Flags().GetBool("debug")
	if debug {
		args = append(args, "--debug")
	}

	sessionID, _ := cmd.Flags().GetString("session")

	switch {
	case sessionID != "":
		args = append(args, "--session", sessionID)
	default:
		args = append(args, "--continue")
	}

	return args
}
