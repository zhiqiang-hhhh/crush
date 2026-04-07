package dialog

import (
	"image"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/projects"
	"github.com/charmbracelet/crush/internal/search"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/util"
	uv "github.com/charmbracelet/ultraviolet"
)

// SessionSearchID is the identifier for the session search dialog.
const SessionSearchID = "session_search"

// sessionSearchResultMsg is an internal message carrying search results.
type sessionSearchResultMsg struct {
	results []search.SearchResult
	err     error
}

// sessionPreviewMsg carries preview lines for a session.
type sessionPreviewMsg struct {
	sessionID string
	lines     []string
}

// SessionSearch is a cross-project session search dialog.
type SessionSearch struct {
	com     *common.Common
	help    help.Model
	list    *list.FilterableList
	input   textinput.Model
	results []search.SearchResult
	loading bool

	// preview state
	preview    []string
	previewSID string

	keyMap struct {
		Select   key.Binding
		Next     key.Binding
		Previous key.Binding
		UpDown   key.Binding
		Delete   key.Binding
		Close    key.Binding
	}
}

var _ Dialog = (*SessionSearch)(nil)

// NewSessionSearch creates a new SessionSearch dialog.
func NewSessionSearch(com *common.Common) *SessionSearch {
	s := new(SessionSearch)
	s.com = com

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	s.help = h

	s.list = list.NewFilterableList()
	s.list.Focus()

	s.input = textinput.New()
	s.input.SetVirtualCursor(false)
	s.input.Placeholder = "Search sessions…"
	s.input.SetStyles(com.Styles.TextInput)
	s.input.Focus()

	s.keyMap.Select = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "open"),
	)
	s.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "next"),
	)
	s.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "previous"),
	)
	s.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑↓", "navigate"),
	)
	s.keyMap.Delete = key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "delete"),
	)
	s.keyMap.Close = CloseKey

	s.loading = true
	return s
}

// InitialSearchCmd returns a command that updates the FTS5 index and
// performs the initial empty-query search (all sessions).
func (s *SessionSearch) InitialSearchCmd() tea.Cmd {
	return func() tea.Msg {
		projs, err := projects.List()
		if err != nil {
			return sessionSearchResultMsg{err: err}
		}
		sp := toSearchProjects(projs)
		_ = search.UpdateIndex(sp)
		results, err := search.Search(sp, "")
		if err != nil {
			return sessionSearchResultMsg{err: err}
		}
		activeIDs := s.com.Mux.ActiveCrushSessions()
		search.MarkActive(results, activeIDs)
		search.SortResults(results)
		return sessionSearchResultMsg{results: results}
	}
}

// ID implements Dialog.
func (s *SessionSearch) ID() string {
	return SessionSearchID
}

// HandleMsg implements Dialog.
func (s *SessionSearch) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case sessionSearchResultMsg:
		s.loading = false
		if msg.err != nil {
			return ActionCmd{util.ReportError(msg.err)}
		}
		s.results = msg.results
		s.list.SetItems(searchResultItems(s.com.Styles, s.results...)...)
		s.list.SelectFirst()
		s.list.ScrollToTop()
		s.preview = nil
		s.previewSID = ""
		return ActionCmd{s.loadPreviewCmd()}

	case sessionPreviewMsg:
		if msg.sessionID == s.previewSID {
			s.preview = msg.lines
		}
		return nil

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, s.keyMap.Close):
			return ActionClose{}

		case key.Matches(msg, s.keyMap.Delete):
			item := s.list.SelectedItem()
			if item == nil {
				return nil
			}
			resultItem := item.(*SearchResultItem)
			if resultItem.Active {
				return ActionCmd{util.ReportWarn("Cannot delete an active session")}
			}
			idx := s.list.Selected()
			s.removeResult(resultItem.ID())
			s.list.SetItems(searchResultItems(s.com.Styles, s.results...)...)
			if s.list.Len() > 0 {
				s.list.SetSelected(min(idx, s.list.Len()-1))
			}
			s.previewSID = ""
			return ActionCmd{tea.Batch(
				s.deleteSessionCmd(resultItem.DBPath, resultItem.ID()),
				s.loadPreviewCmd(),
			)}

		case key.Matches(msg, s.keyMap.Previous):
			s.list.Focus()
			if s.list.IsSelectedFirst() {
				s.list.SelectLast()
			} else {
				s.list.SelectPrev()
			}
			s.list.ScrollToSelected()
			return ActionCmd{s.loadPreviewCmd()}

		case key.Matches(msg, s.keyMap.Next):
			s.list.Focus()
			if s.list.IsSelectedLast() {
				s.list.SelectFirst()
			} else {
				s.list.SelectNext()
			}
			s.list.ScrollToSelected()
			return ActionCmd{s.loadPreviewCmd()}

		case key.Matches(msg, s.keyMap.Select):
			item := s.list.SelectedItem()
			if item == nil {
				return nil
			}
			resultItem := item.(*SearchResultItem)
			return ActionOpenSearchResult{resultItem.SearchResult}

		default:
			var cmd tea.Cmd
			s.input, cmd = s.input.Update(msg)
			query := s.input.Value()
			s.loading = true
			return ActionCmd{tea.Batch(cmd, s.searchCmd(query))}
		}
	}
	return nil
}

// loadPreviewCmd loads the preview for the currently selected session.
func (s *SessionSearch) loadPreviewCmd() tea.Cmd {
	item := s.list.SelectedItem()
	if item == nil {
		s.preview = nil
		return nil
	}
	resultItem := item.(*SearchResultItem)
	sid := resultItem.SessionID
	dbPath := resultItem.DBPath
	if sid == s.previewSID {
		return nil
	}
	s.previewSID = sid
	return func() tea.Msg {
		lines, _ := search.Preview(dbPath, sid)
		return sessionPreviewMsg{sessionID: sid, lines: lines}
	}
}

// searchCmd returns a tea.Cmd that performs the search in the background.
func (s *SessionSearch) searchCmd(query string) tea.Cmd {
	return func() tea.Msg {
		projs, err := projects.List()
		if err != nil {
			return sessionSearchResultMsg{err: err}
		}
		sp := toSearchProjects(projs)
		results, err := search.Search(sp, query)
		if err != nil {
			return sessionSearchResultMsg{err: err}
		}
		activeIDs := s.com.Mux.ActiveCrushSessions()
		search.MarkActive(results, activeIDs)
		search.SortResults(results)
		return sessionSearchResultMsg{results: results}
	}
}

func toSearchProjects(projs []projects.Project) []search.Project {
	sp := make([]search.Project, len(projs))
	for i, p := range projs {
		sp[i] = search.Project{Path: p.Path, DataDir: p.DataDir}
	}
	return sp
}

// deleteSessionCmd deletes a session from the specified database.
func (s *SessionSearch) deleteSessionCmd(dbPath, id string) tea.Cmd {
	return func() tea.Msg {
		if err := search.DeleteSession(dbPath, id); err != nil {
			return util.NewErrorMsg(err)
		}
		return nil
	}
}

// removeResult removes a result from the local results slice by session ID.
func (s *SessionSearch) removeResult(id string) {
	var newResults []search.SearchResult
	for _, r := range s.results {
		if r.SessionID == id {
			continue
		}
		newResults = append(newResults, r)
	}
	s.results = newResults
}

// Cursor returns the cursor position relative to the dialog.
func (s *SessionSearch) Cursor() *tea.Cursor {
	return InputCursor(s.com.Styles, s.input.Cursor())
}

// Draw implements [Dialog].
// Left panel: standard dialog (RenderContext). Right panel: preview.
// Both drawn directly to uv.Screen so preview content is auto-clipped.
func (s *SessionSearch) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := s.com.Styles

	// Align left edge to chat content area.
	chatArea := s.com.ChatArea
	startX := chatArea.Min.X
	if startX <= area.Min.X {
		startX = area.Min.X + area.Dx()/4
	}
	availW := area.Max.X - startX - 1

	// Left panel: standard dialog layout
	leftWidth := max(0, min(defaultDialogMaxWidth, availW-t.Dialog.View.GetHorizontalBorderSize()))
	dialogWidth := leftWidth
	innerWidth := dialogWidth - t.Dialog.View.GetHorizontalFrameSize()

	// Left panel height: standard dialog height
	totalHeight := max(0, min(defaultDialogHeight, area.Dy()-4))
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()

	s.input.SetWidth(max(0, innerWidth-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1))
	s.list.SetSize(innerWidth, totalHeight-heightOffset)
	s.help.SetWidth(innerWidth)

	cur := s.Cursor()
	rc := NewRenderContext(t, dialogWidth)
	rc.Title = "Search Sessions"
	rc.AddPart(t.Dialog.InputPrompt.Render(s.input.View()))
	rc.AddPart(t.Dialog.List.Height(s.list.Height()).Render(s.list.Render()))
	rc.Help = s.help.View(s)
	leftView := rc.Render()
	_, leftH := lipgloss.Size(leftView)

	// Preview: taller than left panel, fill vertical space with top margin
	const previewTopMargin = 5
	previewH := max(leftH, area.Dy()-4-previewTopMargin)
	rightWidth := max(0, availW-leftWidth)
	previewView := s.buildPreview(rightWidth, previewH)

	// Preview: top pushed down by margin, bottom anchored near screen bottom
	previewStartY := area.Min.Y + previewTopMargin + max(0, (area.Dy()-previewTopMargin-previewH)/2)
	// Left panel centered within the preview height
	leftStartY := previewStartY + max(0, (previewH-leftH)/2)

	// Draw left
	leftRect := image.Rect(startX, leftStartY, startX+leftWidth, leftStartY+leftH)
	uv.NewStyledString(leftView).Draw(scr, leftRect)

	// Draw right
	rightRect := image.Rect(startX+leftWidth, previewStartY, startX+leftWidth+rightWidth, previewStartY+previewH)
	uv.NewStyledString(previewView).Draw(scr, rightRect)

	if cur != nil {
		cur.X += startX
		cur.Y += leftStartY
	}
	return cur
}

// buildPreview builds the preview panel with a rounded border.
func (s *SessionSearch) buildPreview(width, height int) string {
	borderW := max(0, width-2)
	borderH := max(0, height-2)

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(s.com.Styles.Subtle.GetForeground()).
		Width(borderW).
		Height(borderH)

	if len(s.preview) == 0 {
		return border.Render("")
	}
	return border.Render(strings.Join(s.preview, "\n"))
}

// ShortHelp implements [help.KeyMap].
func (s *SessionSearch) ShortHelp() []key.Binding {
	return []key.Binding{
		s.keyMap.UpDown,
		s.keyMap.Delete,
		s.keyMap.Select,
		s.keyMap.Close,
	}
}

// FullHelp implements [help.KeyMap].
func (s *SessionSearch) FullHelp() [][]key.Binding {
	slice := s.ShortHelp()
	m := [][]key.Binding{}
	for i := 0; i < len(slice); i += 4 {
		end := min(i+4, len(slice))
		m = append(m, slice[i:end])
	}
	return m
}
