package model

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/logo"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/workspace"
)

// selectedLargeModel returns the currently selected large language model from
// the agent coordinator, if one exists.
func (m *UI) selectedLargeModel() *workspace.AgentModel {
	if m.com.Workspace.AgentIsReady() {
		model := m.com.Workspace.AgentModel()
		return &model
	}
	return nil
}

// landingView renders the landing page header: centered big logo, dividers,
// project info, and model details. The width is the inner content width
// (without outer padding).
func (m *UI) landingView(width int) string {
	t := m.com.Styles

	crushLogo := logo.LandingRender(t, t.LogoTitleColorA, t.LogoTitleColorB)
	crushLogo = centerText(crushLogo, width)

	divider := styles.ApplyForegroundGrad(t,
		strings.Repeat("─", width),
		t.BgSubtle, t.BgOverlay,
	)

	cwd := common.PrettyPath(t, m.com.Workspace.WorkingDir(), width)
	modelInfo := m.modelInfo(width)

	parts := []string{
		crushLogo,
		"",
		divider,
		"",
		cwd,
		modelInfo,
	}

	mcpInfo := m.landingMCPInfo(width)
	if mcpInfo != "" {
		parts = append(parts, "", mcpInfo)
	}

	parts = append(parts, "", divider)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		parts...,
	)
}

// centerText horizontally centers a multi-line string within the given width.
func centerText(s string, width int) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		w := lipgloss.Width(line)
		if w < width {
			pad := (width - w) / 2
			lines[i] = strings.Repeat(" ", pad) + line
		}
	}
	return strings.Join(lines, "\n")
}

// landingLoadingView renders a spinner + loading text shown on the landing page
// while a session is being loaded.
func (m *UI) landingLoadingView(width int) string {
	t := m.com.Styles
	text := m.loadingSpinner.View() + " Loading session…"
	styled := lipgloss.NewStyle().Foreground(t.FgMuted).Render(text)
	return centerText(styled, width)
}
