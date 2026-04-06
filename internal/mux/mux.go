// Package mux provides a thin abstraction over terminal multiplexers
// (tmux, psmux) so that crush features like session-fork-to-new-window
// work identically regardless of which multiplexer is running.
package mux

import (
	"context"
	"os"
	"os/exec"
	"time"
)

// Mux represents a terminal multiplexer that can manage windows and
// pane-level options.  A nil *Mux means no multiplexer is available.
type Mux struct {
	bin string // absolute path or bare name of the mux binary
}

// Detect returns a *Mux for the current environment, or nil if crush
// is not running inside a supported multiplexer.
func Detect() *Mux {
	if os.Getenv("TMUX") == "" {
		return nil
	}
	// Both tmux and psmux set $TMUX and accept the same command syntax.
	for _, name := range []string{"tmux", "psmux"} {
		if p, err := exec.LookPath(name); err == nil {
			return &Mux{bin: p}
		}
	}
	return nil
}

// Available reports whether a multiplexer was detected.
func (m *Mux) Available() bool {
	return m != nil
}

// NewWindow opens a new multiplexer window running the given command
// in the specified working directory.
func (m *Mux) NewWindow(cwd string, args ...string) error {
	if m == nil {
		return nil
	}
	cmdArgs := []string{"new-window", "-c", cwd}
	cmdArgs = append(cmdArgs, args...)
	return m.run(cmdArgs...)
}

// SetPaneOption sets a pane-level user option (e.g. @crush_session).
func (m *Mux) SetPaneOption(key, value string) {
	if m == nil {
		return
	}
	go func() {
		_ = m.run("set-option", "-p", key, value)
	}()
}

// GetPaneOption reads a pane-level user option.  Returns "" on any error.
func (m *Mux) GetPaneOption(key string) string {
	if m == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, m.bin, "show-option", "-pqv", key).Output()
	if err != nil {
		return ""
	}
	// trim trailing newline
	s := string(out)
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	return s
}

// PaneCwd returns the current working directory of the active pane.
func (m *Mux) PaneCwd() string {
	if m == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, m.bin, "display-message", "-p", "#{pane_current_path}").Output()
	if err != nil {
		return ""
	}
	s := string(out)
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	return s
}

func (m *Mux) run(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, m.bin, args...).Run()
}
