package dialog

import (
	"fmt"
	"image"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/askuser"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/charmtone"
)

const (
	AskUserID              = "ask_user"
	askUserDialogMaxWidth  = 80
	askUserDialogMaxHeight = 24
	askUserBodyMaxWidth    = 120
	askUserBodyMaxHeight   = 40
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
	vp      viewport.Model
	vpDirty bool

	renderedBody      string
	renderedBodyWidth int

	// Mouse selection state for body content.
	mouseDown  bool
	mouseDownX int
	mouseDownY int
	mouseDragX int
	mouseDragY int

	// Body viewport screen position, computed in Draw().
	bodyScreenX int
	bodyScreenY int
	bodyScreenW int
	bodyScreenH int

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
	ScrollUp    key.Binding
	ScrollDown  key.Binding
	Copy        key.Binding
}

var _ Dialog = (*AskUser)(nil)

// RequestID returns the ID of the underlying question request.
func (a *AskUser) RequestID() string { return a.request.ID }

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
				key.WithKeys("ctrl+g"),
				key.WithHelp("ctrl+g", "dismiss"),
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
				key.WithKeys("ctrl+g"),
				key.WithHelp("ctrl+g", "back to options"),
			),
			ScrollUp: key.NewBinding(
				key.WithKeys("shift+up", "shift+k"),
				key.WithHelp("shift+↑", "scroll up"),
			),
			ScrollDown: key.NewBinding(
				key.WithKeys("shift+down", "shift+j"),
				key.WithHelp("shift+↓", "scroll down"),
			),
			Copy: key.NewBinding(
				key.WithKeys("c"),
				key.WithHelp("c", "copy plan"),
			),
		},
		vpDirty: true,
		vp: func() viewport.Model {
			vp := viewport.New()
			vp.KeyMap = viewport.KeyMap{
				Up:           key.NewBinding(key.WithKeys("shift+up", "shift+k")),
				Down:         key.NewBinding(key.WithKeys("shift+down", "shift+j")),
				Left:         key.NewBinding(key.WithDisabled()),
				Right:        key.NewBinding(key.WithDisabled()),
				PageUp:       key.NewBinding(key.WithDisabled()),
				PageDown:     key.NewBinding(key.WithDisabled()),
				HalfPageUp:   key.NewBinding(key.WithDisabled()),
				HalfPageDown: key.NewBinding(key.WithDisabled()),
			}
			return vp
		}(),
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
		if key.Matches(msg, a.keyMap.ScrollUp) || key.Matches(msg, a.keyMap.ScrollDown) {
			a.vp, _ = a.vp.Update(msg)
			return nil
		}
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
	case tea.MouseClickMsg:
		if a.request.Body != "" {
			cx, cy, ok := a.screenToBodyCoords(msg.X, msg.Y)
			if ok {
				a.mouseDown = true
				a.mouseDownX = cx
				a.mouseDownY = cy
				a.mouseDragX = cx
				a.mouseDragY = cy
				a.vpDirty = true
			}
		}
	case tea.MouseMotionMsg:
		if a.mouseDown && a.request.Body != "" {
			cx, cy, _ := a.screenToBodyCoords(msg.X, msg.Y)
			a.mouseDragX = cx
			a.mouseDragY = cy
			a.vpDirty = true
		}
	case tea.MouseReleaseMsg:
		if a.mouseDown {
			a.mouseDown = false
			if a.hasBodyHighlight() {
				text := a.highlightedContent()
				a.mouseDownX = 0
				a.mouseDownY = 0
				a.mouseDragX = 0
				a.mouseDragY = 0
				a.vpDirty = true
				if text != "" {
					return ActionCmd{common.CopyToClipboard(text, "Copied to clipboard")}
				}
			}
		}
	case tea.MouseWheelMsg:
		a.vp, _ = a.vp.Update(msg)
	}
	return nil
}

func (a *AskUser) dismiss() Action {
	return ActionAskUserResponse{
		Request: a.request,
		Answers: nil,
	}
}

func (a *AskUser) screenToBodyCoords(screenX, screenY int) (int, int, bool) {
	x := screenX - a.bodyScreenX
	y := screenY - a.bodyScreenY
	if x < 0 || y < 0 || x >= a.bodyScreenW || y >= a.bodyScreenH {
		return x, y + a.vp.YOffset(), false
	}
	return x, y + a.vp.YOffset(), true
}

func (a *AskUser) hasBodyHighlight() bool {
	return a.mouseDownX != a.mouseDragX || a.mouseDownY != a.mouseDragY
}

func (a *AskUser) normalizedHighlightRange() (int, int, int, int) {
	sy, sx := a.mouseDownY, a.mouseDownX
	ey, ex := a.mouseDragY, a.mouseDragX
	if sy > ey || (sy == ey && sx > ex) {
		sy, sx, ey, ex = ey, ex, sy, sx
	}
	return sy, sx, ey, ex
}

func (a *AskUser) highlightedContent() string {
	if a.renderedBody == "" {
		return ""
	}
	startLine, startCol, endLine, endCol := a.normalizedHighlightRange()
	area := image.Rect(0, 0, a.renderedBodyWidth, lipgloss.Height(a.renderedBody))
	return list.HighlightContent(a.renderedBody, area, startLine, startCol, endLine, endCol)
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
	case key.Matches(msg, a.keyMap.Copy):
		if a.request.Body != "" {
			return ActionCmd{common.CopyToClipboard(a.request.Body, "Plan copied to clipboard")}
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
		dialogStyle.GetPaddingTop() +
		dialogStyle.GetMarginTop() +
		dialogStyle.GetBorderTopSize()
	return cur
}

// renderBody renders the body content as markdown.
func (a *AskUser) renderBody(width int) string {
	renderer := common.MarkdownRenderer(a.com.Styles, width)
	result, err := renderer.Render(a.request.Body)
	if err != nil {
		return a.request.Body
	}
	return strings.TrimSuffix(result, "\n")
}

// Draw implements [Dialog].
func (a *AskUser) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := a.com.Styles

	hasBody := a.request.Body != ""
	dialogMaxW := askUserDialogMaxWidth
	dialogMaxH := askUserDialogMaxHeight
	if hasBody {
		dialogMaxW = askUserBodyMaxWidth
		dialogMaxH = askUserBodyMaxHeight
	}
	width := max(0, min(dialogMaxW, area.Dx()))
	maxHeight := max(0, min(dialogMaxH, area.Dy()-4))

	dialogStyle := a.dialogViewStyle()
	innerWidth := width - dialogStyle.GetHorizontalFrameSize()

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
	header := titleLine + "\n" + separator

	// Build the interactive section (options, text input) — kept outside the viewport.
	var interactiveParts []string

	if a.textMode {
		a.input.SetWidth(innerWidth - t.Dialog.InputPrompt.GetHorizontalFrameSize() - 1)
		inputView := t.Dialog.InputPrompt.Render(a.input.View())
		interactiveParts = append(interactiveParts, inputView)

		if len(a.request.Options) > 0 {
			hintStyle := lipgloss.NewStyle().
				Foreground(t.FgSubtle).
				Padding(0, 1).
				Width(innerWidth)
			interactiveParts = append(interactiveParts, hintStyle.Render("Press Esc to go back to options"))
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
		interactiveParts = append(interactiveParts, strings.Join(optParts, "\n\n"))

		if a.request.AllowText {
			hintStyle := lipgloss.NewStyle().
				Foreground(t.FgSubtle).
				Padding(0, 1).
				Width(innerWidth)
			interactiveParts = append(interactiveParts, hintStyle.Render("Press t to type a custom answer"))
		}
	}

	a.help.SetWidth(innerWidth)
	helpView := a.help.View(a)

	interactiveContent := strings.Join(interactiveParts, "\n")

	// Calculate how much space the question viewport gets.
	headerHeight := lipgloss.Height(header)
	interactiveHeight := lipgloss.Height(interactiveContent)
	helpHeight := lipgloss.Height(helpView)
	frameHeight := dialogStyle.GetVerticalFrameSize() + 1 // +1 for blank line before help
	fixedHeight := headerHeight + interactiveHeight + helpHeight + frameHeight

	questionStyle := lipgloss.NewStyle().Width(innerWidth).PaddingBottom(1)
	questionRendered := questionStyle.Render(a.request.Question)
	questionHeight := lipgloss.Height(questionRendered)

	if hasBody {
		bodyWidth := innerWidth - 1
		if a.vpDirty || a.renderedBodyWidth != bodyWidth {
			a.renderedBody = a.renderBody(bodyWidth)
			a.renderedBodyWidth = bodyWidth
		}
		bodyHeight := lipgloss.Height(a.renderedBody)
		availableForBody := max(maxHeight-fixedHeight-questionHeight-1, 3)

		needsScroll := bodyHeight > availableForBody
		vpWidth := innerWidth
		if needsScroll {
			vpWidth = innerWidth - 1
		}

		// Apply mouse highlight to body content.
		bodyContent := a.renderedBody
		if a.hasBodyHighlight() {
			startLine, startCol, endLine, endCol := a.normalizedHighlightRange()
			hlArea := image.Rect(0, 0, vpWidth, bodyHeight)
			bodyContent = list.Highlight(bodyContent, hlArea, startLine, startCol, endLine, endCol, list.ToHighlighter(t.TextSelection))
		}

		a.vp.SetWidth(vpWidth)
		a.vp.SetHeight(availableForBody)
		if a.vpDirty || a.vp.Width() != vpWidth {
			a.vp.SetContent(bodyContent)
			a.vpDirty = false
		}

		var bodyView string
		if needsScroll {
			scrollbar := common.Scrollbar(t, availableForBody, a.vp.TotalLineCount(), availableForBody, a.vp.YOffset())
			bodyView = lipgloss.JoinHorizontal(lipgloss.Top, a.vp.View(), scrollbar)
		} else {
			bodyView = a.vp.View()
		}

		parts := []string{header, questionRendered, bodyView, ""}
		if interactiveContent != "" {
			parts = append(parts, interactiveContent)
		}
		parts = append(parts, "", helpView)

		innerContent := lipgloss.JoinVertical(lipgloss.Left, parts...)
		view := dialogStyle.Render(innerContent)

		// Compute body viewport screen position for mouse coord translation.
		viewWidth, viewHeight := lipgloss.Size(view)
		center := common.CenterRect(area, viewWidth, viewHeight)
		a.bodyScreenX = center.Min.X + dialogStyle.GetBorderLeftSize() + dialogStyle.GetPaddingLeft()
		a.bodyScreenY = center.Min.Y + dialogStyle.GetBorderTopSize() + dialogStyle.GetPaddingTop() + headerHeight + questionHeight
		a.bodyScreenW = vpWidth
		a.bodyScreenH = availableForBody

		cur := a.Cursor()
		if cur != nil {
			// header + question + bodyView + empty separator line
			cur.Y += headerHeight + questionHeight + lipgloss.Height(bodyView) + 1
		}
		DrawCenterCursor(scr, area, view, cur)
		return cur
	}

	availableForQuestion := max(maxHeight-fixedHeight, 3)

	needsScroll := questionHeight > availableForQuestion
	vpWidth := innerWidth
	if needsScroll {
		vpWidth = innerWidth - 1
	}

	var questionView string
	if needsScroll {
		a.vp.SetWidth(vpWidth)
		a.vp.SetHeight(availableForQuestion)
		if a.vpDirty || a.vp.Width() != vpWidth {
			a.vp.SetContent(questionRendered)
			a.vpDirty = false
		}
		scrollbar := common.Scrollbar(t, availableForQuestion, a.vp.TotalLineCount(), availableForQuestion, a.vp.YOffset())
		questionView = lipgloss.JoinHorizontal(lipgloss.Top, a.vp.View(), scrollbar)
	} else {
		questionView = questionRendered
	}

	parts := []string{header, questionView}
	if interactiveContent != "" {
		parts = append(parts, interactiveContent)
	}
	parts = append(parts, "", helpView)

	innerContent := lipgloss.JoinVertical(lipgloss.Left, parts...)
	view := dialogStyle.Render(innerContent)
	cur := a.Cursor()
	if cur != nil {
		cur.Y += headerHeight + lipgloss.Height(questionView)
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
	if a.request.Body != "" {
		bindings = append(bindings, a.keyMap.Copy)
	}
	bindings = append(bindings, a.keyMap.Close)
	return bindings
}

// FullHelp implements [help.KeyMap].
func (a *AskUser) FullHelp() [][]key.Binding {
	return [][]key.Binding{a.ShortHelp()}
}
