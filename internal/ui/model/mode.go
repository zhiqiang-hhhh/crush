package model

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/util"
)

type modeCommand struct {
	showCurrent bool
	mode        session.SessionMode
}

func normalizeSessionMode(mode session.SessionMode) session.SessionMode {
	switch mode {
	case session.SessionModePlan:
		return session.SessionModePlan
	case session.SessionModeShell:
		return session.SessionModeShell
	}
	return session.SessionModeBuild
}

func modeAgentID(mode session.SessionMode) string {
	if normalizeSessionMode(mode) == session.SessionModePlan {
		return config.AgentPlan
	}
	return config.AgentCoder
}

func parseModeCommand(input string) (modeCommand, error, bool) {
	parts := strings.Fields(strings.TrimSpace(input))
	if len(parts) == 0 {
		return modeCommand{}, nil, false
	}

	switch parts[0] {
	case "/plan":
		if len(parts) != 1 {
			return modeCommand{}, fmt.Errorf("usage: /plan"), true
		}
		return modeCommand{mode: session.SessionModePlan}, nil, true
	case "/build":
		if len(parts) != 1 {
			return modeCommand{}, fmt.Errorf("usage: /build"), true
		}
		return modeCommand{mode: session.SessionModeBuild}, nil, true
	case "/shell":
		if len(parts) != 1 {
			return modeCommand{}, fmt.Errorf("usage: /shell"), true
		}
		return modeCommand{mode: session.SessionModeShell}, nil, true
	case "/mode":
		switch len(parts) {
		case 1:
			return modeCommand{showCurrent: true}, nil, true
		case 2:
			switch parts[1] {
			case string(session.SessionModeBuild):
				return modeCommand{mode: session.SessionModeBuild}, nil, true
			case string(session.SessionModePlan):
				return modeCommand{mode: session.SessionModePlan}, nil, true
			case string(session.SessionModeShell):
				return modeCommand{mode: session.SessionModeShell}, nil, true
			default:
				return modeCommand{}, fmt.Errorf("unknown mode %q", parts[1]), true
			}
		default:
			return modeCommand{}, fmt.Errorf("usage: /mode [build|plan|shell]"), true
		}
	default:
		return modeCommand{}, nil, false
	}
}

func (m *UI) activeSessionMode() session.SessionMode {
	if m.session != nil {
		return normalizeSessionMode(m.session.Mode)
	}
	return normalizeSessionMode(m.draftMode)
}

func (m *UI) applySessionMode(mode session.SessionMode) {
	mode = normalizeSessionMode(mode)
	m.draftMode = mode
	if m.session != nil {
		m.session.Mode = mode
	}
}

func (m *UI) persistSessionMode(mode session.SessionMode) tea.Cmd {
	if m.session == nil {
		return nil
	}

	sessionID := m.session.ID
	mode = normalizeSessionMode(mode)
	return func() tea.Msg {
		_, err := m.com.App.Sessions.SetMode(context.Background(), sessionID, mode)
		if err != nil {
			return util.NewErrorMsg(err)
		}
		return nil
	}
}

func (m *UI) handleModeCommand(input string) tea.Cmd {
	parsed, err, ok := parseModeCommand(input)
	if !ok {
		return nil
	}
	if err != nil {
		return util.ReportWarn(err.Error())
	}
	if parsed.showCurrent {
		return util.ReportInfo(fmt.Sprintf("Current mode: %s. Available: build, plan, shell.", m.activeSessionMode()))
	}

	mode := normalizeSessionMode(parsed.mode)
	m.applySessionMode(mode)
	return tea.Batch(
		m.persistSessionMode(mode),
		util.ReportInfo(fmt.Sprintf("Mode changed to %s.", mode)),
	)
}
