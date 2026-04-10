package dialog

import (
	"image"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aymanbagabas/go-udiff"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	uv "github.com/charmbracelet/ultraviolet"
)

const DiffPreviewID = "diff-preview"

const diffHorizontalScrollStep = 5

type DiffPreview struct {
	com *common.Common

	filePath   string
	oldContent string
	newContent string

	viewport      viewport.Model
	viewportDirty bool

	splitMode   bool
	diffXOffset int
	fullscreen  bool

	// Cached rendered diff content.
	splitDiffContent   string
	unifiedDiffContent string

	// Mouse selection state.
	mouseDown    bool
	startLine    int
	startCol     int
	endLine      int
	endCol       int
	hasSelect    bool
	contentArea  image.Rectangle

	km struct {
		Close       key.Binding
		ToggleMode  key.Binding
		Fullscreen  key.Binding
		Copy        key.Binding
		ScrollLeft  key.Binding
		ScrollRight key.Binding
	}
}

var _ Dialog = (*DiffPreview)(nil)

func NewDiffPreview(com *common.Common, filePath, oldContent, newContent string) *DiffPreview {
	vp := viewport.New()
	vp.KeyMap = viewport.KeyMap{
		Up:           key.NewBinding(key.WithKeys("up", "k")),
		Down:         key.NewBinding(key.WithKeys("down", "j")),
		PageUp:       key.NewBinding(key.WithKeys("pgup")),
		PageDown:     key.NewBinding(key.WithKeys("pgdown")),
		HalfPageUp:   key.NewBinding(key.WithKeys("ctrl+u")),
		HalfPageDown: key.NewBinding(key.WithKeys("ctrl+d")),
		Left:         key.NewBinding(key.WithDisabled()),
		Right:        key.NewBinding(key.WithDisabled()),
	}

	d := &DiffPreview{
		com:           com,
		filePath:      filePath,
		oldContent:    oldContent,
		newContent:    newContent,
		viewport:      vp,
		viewportDirty: true,
		splitMode:     true,
		startLine:     -1,
	}
	d.km.Close = key.NewBinding(key.WithKeys("ctrl+g", "q", "esc"))
	d.km.ToggleMode = key.NewBinding(key.WithKeys("t"))
	d.km.Fullscreen = key.NewBinding(key.WithKeys("f"))
	d.km.Copy = key.NewBinding(key.WithKeys("c"))
	d.km.ScrollLeft = key.NewBinding(key.WithKeys("shift+left", "h"))
	d.km.ScrollRight = key.NewBinding(key.WithKeys("shift+right", "l"))
	return d
}

func (d *DiffPreview) ID() string { return DiffPreviewID }

func (d *DiffPreview) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if key.Matches(msg, d.km.Close) {
			return ActionClose{}
		}
		if key.Matches(msg, d.km.ToggleMode) {
			d.splitMode = !d.splitMode
			d.viewportDirty = true
			return nil
		}
		if key.Matches(msg, d.km.Fullscreen) {
			d.fullscreen = !d.fullscreen
			d.viewportDirty = true
			return nil
		}
		if key.Matches(msg, d.km.Copy) {
			text := d.fullDiffText()
			if d.hasSelect {
				if sel := d.selectedText(); sel != "" {
					text = sel
				}
			}
			return ActionCmd{common.CopyToClipboard(text, "Copied to clipboard")}
		}
		if key.Matches(msg, d.km.ScrollLeft) {
			d.diffXOffset = max(0, d.diffXOffset-diffHorizontalScrollStep)
			d.viewportDirty = true
			return nil
		}
		if key.Matches(msg, d.km.ScrollRight) {
			d.diffXOffset += diffHorizontalScrollStep
			d.viewportDirty = true
			return nil
		}
		d.viewport, _ = d.viewport.Update(msg)
	case tea.MouseWheelMsg:
		d.viewport, _ = d.viewport.Update(msg)
	case tea.MouseClickMsg:
		pt := image.Pt(msg.X, msg.Y)
		if !pt.In(d.contentArea) {
			return ActionClose{}
		}
		col := msg.X - d.contentArea.Min.X
		line := msg.Y - d.contentArea.Min.Y + d.viewport.YOffset()
		d.mouseDown = true
		d.startLine = line
		d.startCol = col
		d.endLine = line
		d.endCol = col
		d.hasSelect = false
	case tea.MouseMotionMsg:
		if !d.mouseDown {
			return nil
		}
		col := msg.X - d.contentArea.Min.X
		line := msg.Y - d.contentArea.Min.Y + d.viewport.YOffset()
		d.endLine = line
		d.endCol = col
		d.hasSelect = d.startLine != d.endLine || d.startCol != d.endCol
	case tea.MouseReleaseMsg:
		if !d.mouseDown {
			return nil
		}
		d.mouseDown = false
		if d.hasSelect {
			if text := d.selectedText(); text != "" {
				d.hasSelect = false
				return ActionCmd{common.CopyToClipboard(text, "Selected text copied to clipboard")}
			}
		}
	}
	return nil
}

func (d *DiffPreview) normalizedSelection() (sLine, sCol, eLine, eCol int) {
	sLine, sCol = d.startLine, d.startCol
	eLine, eCol = d.endLine, d.endCol
	if sLine > eLine || (sLine == eLine && sCol > eCol) {
		sLine, sCol, eLine, eCol = eLine, eCol, sLine, sCol
	}
	return
}

func (d *DiffPreview) selectedText() string {
	sLine, sCol, eLine, eCol := d.normalizedSelection()
	rendered := d.viewport.View()
	w := d.contentArea.Dx()
	h := d.contentArea.Dy()
	area := image.Rect(0, 0, w, h)
	visStartLine := sLine - d.viewport.YOffset()
	visEndLine := eLine - d.viewport.YOffset()
	return strings.TrimRight(list.HighlightContent(rendered, area, visStartLine, sCol, visEndLine, eCol), "\n")
}

func (d *DiffPreview) fullDiffText() string {
	file := fsext.PrettyPath(d.filePath)
	return udiff.Unified("a/"+file, "b/"+file, d.oldContent, d.newContent)
}

func (d *DiffPreview) renderDiff(contentWidth int) string {
	formatter := common.DiffFormatter(d.com.Styles).
		Before(d.filePath, d.oldContent).
		After(d.filePath, d.newContent).
		XOffset(d.diffXOffset).
		Width(contentWidth)

	if d.splitMode {
		formatter = formatter.Split()
		d.splitDiffContent = formatter.String()
		return d.splitDiffContent
	}

	formatter = formatter.Unified()
	d.unifiedDiffContent = formatter.String()
	return d.unifiedDiffContent
}

func (d *DiffPreview) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles

	maxWidth := area.Dx()
	maxHeight := area.Dy()
	if !d.fullscreen {
		maxWidth = min(maxWidth, area.Dx()-4)
		maxHeight = min(maxHeight, area.Dy()-4)
	}

	dialogStyle := t.Dialog.View.Width(maxWidth).Padding(0, 1)
	contentWidth := maxWidth - dialogStyle.GetHorizontalFrameSize()

	title := common.DialogTitle(t, "Diff: "+d.filePath, contentWidth-t.Dialog.Title.GetHorizontalFrameSize(), t.Primary, t.Secondary)
	titleRendered := t.Dialog.Title.Render(title)
	titleHeight := lipgloss.Height(titleRendered)

	modeStr := "split"
	if !d.splitMode {
		modeStr = "unified"
	}
	helpView := t.Dialog.HelpView.Width(contentWidth).Render(
		"esc/q: close · j/k: scroll · h/l: pan · t: " + modeStr + " · f: fullscreen · c: copy · drag: select",
	)
	helpHeight := lipgloss.Height(helpView)

	frameHeight := dialogStyle.GetVerticalFrameSize() + 2
	availableHeight := max(3, maxHeight-titleHeight-helpHeight-frameHeight)

	diffContent := d.renderDiff(contentWidth - 1)

	if d.viewportDirty || d.viewport.Width() != contentWidth-1 {
		d.viewport.SetWidth(contentWidth - 1)
		d.viewport.SetHeight(availableHeight)
		d.viewport.SetContent(diffContent)
		d.viewportDirty = false
	} else {
		d.viewport.SetHeight(availableHeight)
	}

	content := d.viewport.View()

	// Apply mouse selection highlight.
	if d.hasSelect {
		sLine, sCol, eLine, eCol := d.normalizedSelection()
		visStartLine := sLine - d.viewport.YOffset()
		visEndLine := eLine - d.viewport.YOffset()
		w := contentWidth - 1
		h := availableHeight
		hlArea := image.Rect(0, 0, w, h)
		content = list.Highlight(content, hlArea, visStartLine, sCol, visEndLine, eCol, list.ToHighlighter(t.TextSelection))
	}

	needsScrollbar := d.viewport.TotalLineCount() > availableHeight
	if needsScrollbar {
		scrollbar := common.Scrollbar(t, availableHeight, d.viewport.TotalLineCount(), availableHeight, d.viewport.YOffset())
		content = lipgloss.JoinHorizontal(lipgloss.Top, content, scrollbar)
	}

	parts := []string{titleRendered, "", content, "", helpView}
	innerContent := lipgloss.JoinVertical(lipgloss.Left, parts...)
	rendered := dialogStyle.Render(innerContent)

	width, height := lipgloss.Size(rendered)
	center := common.CenterRect(area, width, height)

	pad := dialogStyle.GetHorizontalPadding()
	contentTopY := center.Min.Y + titleHeight + 2
	d.contentArea = image.Rect(
		center.Min.X+pad,
		contentTopY,
		center.Min.X+pad+contentWidth-1,
		contentTopY+availableHeight,
	)

	uv.NewStyledString(rendered).Draw(scr, center)
	return nil
}
