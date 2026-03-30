package dialog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/askuser"
	"github.com/charmbracelet/crush/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/charmtone"
)

const (
	AskUserID              = "ask_user"
	askUserDialogMaxWidth  = 80
	askUserDialogMaxHeight = 24
)

// ActionAskUserResponse is sent when the user answers the question.
type ActionAskUserResponse struct {
	Request askuser.QuestionRequest
	Answers []string
}

// AskUser represents a dialog for agent questions.
type AskUser struct {
	com     *common.Common
	request askuser.QuestionRequest
	help    help.Model
	input   textinput.Model

	selectedOption int
	selected       map[int]bool // for multi-select
	textMode       bool         // true when in custom text input mode (while options exist)

	keyMap askUserKeyMap
}

type askUserKeyMap struct {
	Select      key.Binding
	Next        key.Binding
	Previous    key.Binding
	UpDown      key.Binding
	Close       key.Binding
	Toggle      key.Binding
	CustomInput key.Binding
	Back        key.Binding
}

var _ Dialog = (*AskUser)(nil)

// NewAskUser creates a new ask_user dialog.
func NewAskUser(com *common.Common, req askuser.QuestionRequest) *AskUser {
	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()

	ti := textinput.New()
	ti.SetVirtualCursor(false)
	ti.Placeholder = "Type your answer..."
	tiStyles := com.Styles.TextInput
	tiStyles.Cursor.Shape = tea.CursorBar
	tiStyles.Cursor.Color = lipgloss.Color("#00ff00")
	ti.SetStyles(tiStyles)
	ti.CharLimit = 500

	hasOptions := len(req.Options) > 0
	isTextOnly := !hasOptions

	if isTextOnly {
		ti.Focus()
	}

	return &AskUser{
		com:      com,
		request:  req,
		help:     h,
		input:    ti,
		selected: make(map[int]bool),
		textMode: isTextOnly,
		keyMap: askUserKeyMap{
			Select: key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("enter", "confirm"),
			),
			Next: key.NewBinding(
				key.WithKeys("down", "ctrl+n", "j"),
				key.WithHelp("↓", "next"),
			),
			Previous: key.NewBinding(
				key.WithKeys("up", "ctrl+p", "k"),
				key.WithHelp("↑", "previous"),
			),
			UpDown: key.NewBinding(
				key.WithKeys("up", "down"),
				key.WithHelp("↑/↓", "choose"),
			),
			Close: key.NewBinding(
				key.WithKeys("esc"),
				key.WithHelp("esc", "dismiss"),
			),
			Toggle: key.NewBinding(
				key.WithKeys("space", " "),
				key.WithHelp("space", "toggle"),
			),
			CustomInput: key.NewBinding(
				key.WithKeys("t"),
				key.WithHelp("t", "type answer"),
			),
			Back: key.NewBinding(
				key.WithKeys("esc"),
				key.WithHelp("esc", "back to options"),
			),
		},
	}
}

// ID implements [Dialog].
func (*AskUser) ID() string {
	return AskUserID
}

// HandleMsg implements [Dialog].
func (a *AskUser) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if a.textMode && len(a.request.Options) > 0 {
			return a.handleCustomTextMode(msg)
		}
		if a.textMode {
			return a.handleTextOnlyMode(msg)
		}
		if a.request.Multi {
			return a.handleMultiSelectMode(msg)
		}
		return a.handleSingleSelectMode(msg)
	}
	return nil
}

func (a *AskUser) dismiss() Action {
	return ActionAskUserResponse{
		Request: a.request,
		Answers: nil,
	}
}

func (a *AskUser) handleSingleSelectMode(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, a.keyMap.Close):
		return a.dismiss()
	case key.Matches(msg, a.keyMap.Next):
		a.selectedOption = (a.selectedOption + 1) % len(a.request.Options)
	case key.Matches(msg, a.keyMap.Previous):
		a.selectedOption = (a.selectedOption + len(a.request.Options) - 1) % len(a.request.Options)
	case key.Matches(msg, a.keyMap.Select):
		return ActionAskUserResponse{
			Request: a.request,
			Answers: []string{a.request.Options[a.selectedOption].Label},
		}
	case key.Matches(msg, a.keyMap.CustomInput):
		if a.request.AllowText {
			a.textMode = true
			a.input.Focus()
		}
	default:
		if r := msg.String(); len(r) == 1 && r[0] >= '1' && r[0] <= '9' {
			idx := int(r[0] - '1')
			if idx < len(a.request.Options) {
				return ActionAskUserResponse{
					Request: a.request,
					Answers: []string{a.request.Options[idx].Label},
				}
			}
		}
	}
	return nil
}

func (a *AskUser) handleMultiSelectMode(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, a.keyMap.Close):
		return a.dismiss()
	case key.Matches(msg, a.keyMap.Next):
		a.selectedOption = (a.selectedOption + 1) % len(a.request.Options)
	case key.Matches(msg, a.keyMap.Previous):
		a.selectedOption = (a.selectedOption + len(a.request.Options) - 1) % len(a.request.Options)
	case key.Matches(msg, a.keyMap.Toggle):
		a.selected[a.selectedOption] = !a.selected[a.selectedOption]
	case key.Matches(msg, a.keyMap.Select):
		var answers []string
		for i, opt := range a.request.Options {
			if a.selected[i] {
				answers = append(answers, opt.Label)
			}
		}
		if len(answers) == 0 {
			break
		}
		return ActionAskUserResponse{
			Request: a.request,
			Answers: answers,
		}
	case key.Matches(msg, a.keyMap.CustomInput):
		if a.request.AllowText {
			a.textMode = true
			a.input.Focus()
		}
	}
	return nil
}

func (a *AskUser) handleTextOnlyMode(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, a.keyMap.Close):
		return a.dismiss()
	case key.Matches(msg, a.keyMap.Select):
		val := strings.TrimSpace(a.input.Value())
		if val != "" {
			return ActionAskUserResponse{
				Request: a.request,
				Answers: []string{val},
			}
		}
	default:
		var cmd tea.Cmd
		a.input, cmd = a.input.Update(msg)
		if cmd != nil {
			return ActionCmd{cmd}
		}
	}
	return nil
}

func (a *AskUser) handleCustomTextMode(msg tea.KeyPressMsg) Action {
	if key.Matches(msg, a.keyMap.Back) {
		a.textMode = false
		a.input.Blur()
		return nil
	}
	if key.Matches(msg, a.keyMap.Select) {
		val := strings.TrimSpace(a.input.Value())
		if val != "" {
			return ActionAskUserResponse{
				Request: a.request,
				Answers: []string{val},
			}
		}
		return nil
	}
	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	if cmd != nil {
		return ActionCmd{cmd}
	}
	return nil
}

// dialogViewStyle returns the custom view style used by this dialog.
func (a *AskUser) dialogViewStyle() lipgloss.Style {
	return a.com.Styles.Dialog.View.
		BorderForeground(a.com.Styles.FgSubtle).
		Padding(1, 2)
}

// Cursor returns the cursor position relative to the dialog.
func (a *AskUser) Cursor() *tea.Cursor {
	if !a.textMode {
		return nil
	}
	cur := a.input.Cursor()
	if cur == nil {
		return nil
	}
	t := a.com.Styles
	inputStyle := t.Dialog.InputPrompt
	dialogStyle := a.dialogViewStyle()
	cur.X += inputStyle.GetBorderLeftSize() +
		inputStyle.GetMarginLeft() +
		inputStyle.GetPaddingLeft() +
		dialogStyle.GetBorderLeftSize() +
		dialogStyle.GetPaddingLeft() +
		dialogStyle.GetMarginLeft()
	cur.Y += inputStyle.GetBorderTopSize() +
		inputStyle.GetMarginTop() +
		inputStyle.GetPaddingTop() +
		inputStyle.GetBorderBottomSize() +
		inputStyle.GetMarginBottom() +
		inputStyle.GetPaddingBottom() +
		dialogStyle.GetPaddingTop() +
		dialogStyle.GetMarginTop() +
		dialogStyle.GetBorderTopSize()
	return cur
}

// contentHeightAboveInput returns the number of lines rendered above the text
// input in the dialog, including the custom title and separator.
func (a *AskUser) contentHeightAboveInput(innerWidth int) int {
	const titleAndSeparatorHeight = 1
	questionStyle := lipgloss.NewStyle().
		Width(innerWidth).
		PaddingBottom(1)
	question := questionStyle.Render(a.request.Question)
	return titleAndSeparatorHeight + lipgloss.Height(question)
}

// Draw implements [Dialog].
func (a *AskUser) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := a.com.Styles

	width := max(0, min(askUserDialogMaxWidth, area.Dx()))
	height := max(0, min(askUserDialogMaxHeight, area.Dy()))
	_ = height

	rc := NewRenderContext(t, width)
	rc.ViewStyle = a.dialogViewStyle()

	innerWidth := width - rc.ViewStyle.GetHorizontalFrameSize()

	title := "Agent Question"
	if a.request.Header != "" {
		title = a.request.Header
	}
	titleLine := lipgloss.NewStyle().
		Bold(true).
		Foreground(charmtone.Yam).
		Width(innerWidth).
		AlignHorizontal(lipgloss.Center).
		Render(title)
	separator := lipgloss.NewStyle().
		Foreground(t.FgSubtle).
		Width(innerWidth).
		AlignHorizontal(lipgloss.Center).
		Render(strings.Repeat("─", innerWidth))
	rc.AddPart(titleLine + "\n" + separator)

	questionStyle := lipgloss.NewStyle().
		Width(innerWidth).
		PaddingBottom(1)
	rc.AddPart(questionStyle.Render(a.request.Question))

	if a.textMode {
		a.input.SetWidth(innerWidth - t.Dialog.InputPrompt.GetHorizontalFrameSize() - 1)
		inputView := t.Dialog.InputPrompt.Render(a.input.View())
		rc.AddPart(inputView)

		if len(a.request.Options) > 0 {
			hintStyle := lipgloss.NewStyle().
				Foreground(t.FgSubtle).
				Padding(0, 1).
				Width(innerWidth)
			rc.AddPart(hintStyle.Render("Press Esc to go back to options"))
		}
	} else if len(a.request.Options) > 0 {
		accent := charmtone.Yam
		normalLabelStyle := lipgloss.NewStyle().
			Foreground(t.FgBase)
		focusedLabelStyle := lipgloss.NewStyle().
			Foreground(accent).
			Bold(true)
		normalDescStyle := lipgloss.NewStyle().
			Foreground(t.FgMuted)
		focusedDescStyle := lipgloss.NewStyle().
			Foreground(t.FgHalfMuted)
		checkedStyle := lipgloss.NewStyle().Foreground(accent)
		uncheckedStyle := lipgloss.NewStyle().Foreground(t.FgMuted)

		var optParts []string
		for i, opt := range a.request.Options {
			isFocused := i == a.selectedOption

			var prefix string
			if a.request.Multi {
				if a.selected[i] {
					prefix = checkedStyle.Render("◉") + " "
				} else {
					prefix = uncheckedStyle.Render("○") + " "
				}
			}

			var numStr string
			if isFocused {
				numStr = focusedLabelStyle.Render(fmt.Sprintf("%d.", i+1))
			} else {
				numStr = normalLabelStyle.Render(fmt.Sprintf("%d.", i+1))
			}

			var labelStr string
			if isFocused {
				labelStr = focusedLabelStyle.Render(opt.Label)
			} else {
				labelStr = normalLabelStyle.Render(opt.Label)
			}

			line := fmt.Sprintf("  %s%s %s", prefix, numStr, labelStr)

			if opt.Description != "" {
				var descStr string
				indent := "     "
				if a.request.Multi {
					indent += "  "
				}
				if isFocused {
					descStr = focusedDescStyle.Render(opt.Description)
				} else {
					descStr = normalDescStyle.Render(opt.Description)
				}
				line += "\n" + indent + descStr
			}

			optParts = append(optParts, line)
		}
		rc.AddPart(strings.Join(optParts, "\n"))

		if a.request.AllowText {
			hintStyle := lipgloss.NewStyle().
				Foreground(t.FgSubtle).
				Padding(0, 1).
				Width(innerWidth)
			rc.AddPart(hintStyle.Render("Press t to type a custom answer"))
		}
	}

	a.help.SetWidth(innerWidth)
	rc.Help = a.help.View(a)

	view := rc.Render()
	cur := a.Cursor()
	if cur != nil {
		cur.Y += a.contentHeightAboveInput(innerWidth)
	}
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// ShortHelp implements [help.KeyMap].
func (a *AskUser) ShortHelp() []key.Binding {
	if a.textMode {
		bindings := []key.Binding{a.keyMap.Select}
		if len(a.request.Options) > 0 {
			bindings = append(bindings, a.keyMap.Back)
		} else {
			bindings = append(bindings, a.keyMap.Close)
		}
		return bindings
	}
	if a.request.Multi {
		bindings := []key.Binding{a.keyMap.UpDown, a.keyMap.Toggle, a.keyMap.Select}
		if a.request.AllowText {
			bindings = append(bindings, a.keyMap.CustomInput)
		}
		bindings = append(bindings, a.keyMap.Close)
		return bindings
	}
	bindings := []key.Binding{a.keyMap.UpDown, a.keyMap.Select}
	if a.request.AllowText {
		bindings = append(bindings, a.keyMap.CustomInput)
	}
	bindings = append(bindings, a.keyMap.Close)
	return bindings
}

// FullHelp implements [help.KeyMap].
func (a *AskUser) FullHelp() [][]key.Binding {
	return [][]key.Binding{a.ShortHelp()}
}
