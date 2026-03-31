package model

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/shell"
	"github.com/charmbracelet/crush/internal/ui/util"
)

func (m *UI) sendShellCommand(command string) tea.Cmd {
	var cmds []tea.Cmd
	mode := m.activeSessionMode()
	if !m.hasSession() {
		newSession, err := m.com.App.Sessions.Create(context.Background(), "New Session")
		if err != nil {
			return util.ReportError(err)
		}
		if m.forceCompactMode {
			m.isCompact = true
		}
		if newSession.ID != "" {
			newSession.Mode = mode
			m.session = &newSession
			cmds = append(cmds, m.loadSession(newSession.ID))
		}
		m.setState(uiChat, m.focus)
	}
	if m.session != nil {
		m.session.Mode = mode
		if mode != session.SessionModeBuild {
			cmds = append(cmds, m.persistSessionMode(mode))
		}
	}
	if m.session == nil {
		return tea.Batch(append(cmds, util.ReportError(fmt.Errorf("failed to create session")))...)
	}

	sessionID := m.session.ID
	sh := m.sessionShell(sessionID)
	cmds = append(cmds, func() tea.Msg {
		ctx := context.Background()
		_, err := m.com.App.Messages.Create(ctx, sessionID, message.CreateMessageParams{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: command}},
		})
		if err != nil {
			return util.NewErrorMsg(err)
		}

		stdout, stderr, execErr := sh.Exec(ctx, command)
		content := formatShellOutput(command, stdout, stderr, execErr)
		_, err = m.com.App.Messages.Create(ctx, sessionID, message.CreateMessageParams{
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.TextContent{Text: content},
			},
		})
		if err != nil {
			return util.NewErrorMsg(err)
		}
		return nil
	})

	return tea.Batch(cmds...)
}

func (m *UI) sessionShell(sessionID string) *shell.Shell {
	if sh, ok := m.sessionShells[sessionID]; ok {
		return sh
	}

	sh := shell.NewShell(nil)
	m.sessionShells[sessionID] = sh
	return sh
}

func formatShellOutput(command, stdout, stderr string, err error) string {
	var parts []string
	parts = append(parts, "$ "+command)

	output := strings.TrimRight(stdout, "\n")
	if output == "" && strings.TrimSpace(stderr) == "" {
		output = "(no output)"
	}
	if output != "" {
		parts = append(parts, output)
	}

	trimmedStderr := strings.TrimRight(stderr, "\n")
	if trimmedStderr != "" {
		parts = append(parts, "stderr:\n"+trimmedStderr)
	}

	parts = append(parts, fmt.Sprintf("Exit code: %d", shell.ExitCode(err)))
	if err != nil && !shell.IsInterrupt(err) {
		parts = append(parts, fmt.Sprintf("Error: %v", err))
	}

	return strings.Join(parts, "\n\n")
}
