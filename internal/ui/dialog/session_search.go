package dialog

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
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

// SessionSearch is a cross-project session search dialog.
type SessionSearch struct {
	com     *common.Common
	help    help.Model
	list    *list.FilterableList
	input   textinput.Model
	results []search.SearchResult
	loading bool

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
			return ActionCmd{s.deleteSessionCmd(resultItem.DBPath, resultItem.ID())}

		case key.Matches(msg, s.keyMap.Previous):
			s.list.Focus()
			if s.list.IsSelectedFirst() {
				s.list.SelectLast()
			} else {
				s.list.SelectPrev()
			}
			s.list.ScrollToSelected()

		case key.Matches(msg, s.keyMap.Next):
			s.list.Focus()
			if s.list.IsSelectedLast() {
				s.list.SelectFirst()
			} else {
				s.list.SelectNext()
			}
			s.list.ScrollToSelected()

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
func (s *SessionSearch) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := s.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()
	s.input.SetWidth(max(0, innerWidth-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1))
	s.list.SetSize(innerWidth, height-heightOffset)
	s.help.SetWidth(innerWidth)

	cur := s.Cursor()
	rc := NewRenderContext(t, width)
	rc.Title = "Search Sessions"

	inputView := t.Dialog.InputPrompt.Render(s.input.View())
	rc.AddPart(inputView)

	listView := t.Dialog.List.Height(s.list.Height()).Render(s.list.Render())
	rc.AddPart(listView)
	rc.Help = s.help.View(s)

	view := rc.Render()
	DrawCenterCursor(scr, area, view, cur)
	return cur
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
