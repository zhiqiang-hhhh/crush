package dialog

import (
	"image"
	"strings"
	"unicode"

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
	"github.com/charmbracelet/x/ansi"
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
	preview      []string
	previewSID   string
	previewQuery string
	previewRow   int            // vertical scroll offset (visual line index)
	previewRect  image.Rectangle // screen area of preview pane (for mouse hit-test)

	deleting     bool
	alwaysDelete bool

	keyMap struct {
		Select        key.Binding
		Next          key.Binding
		Previous      key.Binding
		UpDown        key.Binding
		Delete        key.Binding
		ConfirmDelete key.Binding
		AlwaysDelete  key.Binding
		CancelDelete  key.Binding
		Close         key.Binding
		PreviewUp     key.Binding
		PreviewDown   key.Binding
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
	s.keyMap.ConfirmDelete = key.NewBinding(
		key.WithKeys("y"),
		key.WithHelp("y", "delete"),
	)
	s.keyMap.AlwaysDelete = key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "always"),
	)
	s.keyMap.CancelDelete = key.NewBinding(
		key.WithKeys("n", "ctrl+g", "esc"),
		key.WithHelp("n", "cancel"),
	)
	s.keyMap.PreviewUp = key.NewBinding(
		key.WithKeys("alt+up", "alt+k"),
	)
	s.keyMap.PreviewDown = key.NewBinding(
		key.WithKeys("alt+down", "alt+j"),
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
			s.scrollPreviewToMatch()
		}
		return nil

	case tea.MouseWheelMsg:
		pt := image.Pt(msg.X, msg.Y)
		if pt.In(s.previewRect) {
			switch msg.Button {
			case tea.MouseWheelUp:
				s.previewRow = max(0, s.previewRow-3)
			case tea.MouseWheelDown:
				s.previewRow += 3
			}
		}
		return nil

	case tea.KeyPressMsg:
		if s.deleting {
			switch {
			case key.Matches(msg, s.keyMap.ConfirmDelete), key.Matches(msg, s.keyMap.AlwaysDelete):
				if key.Matches(msg, s.keyMap.AlwaysDelete) {
					s.alwaysDelete = true
				}
				s.deleting = false
				return s.performDelete()
			case key.Matches(msg, s.keyMap.CancelDelete):
				s.deleting = false
			}
			return nil
		}

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
			if s.alwaysDelete {
				return s.performDelete()
			}
			s.deleting = true
			return nil

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

		case key.Matches(msg, s.keyMap.PreviewUp):
			s.previewRow = max(0, s.previewRow-3)
			return nil
		case key.Matches(msg, s.keyMap.PreviewDown):
			s.previewRow += 3
			return nil

		default:
			var cmd tea.Cmd
			s.input, cmd = s.input.Update(msg)
			query := s.input.Value()
			s.loading = true
			s.previewQuery = query
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

// performDelete deletes the currently selected session.
func (s *SessionSearch) performDelete() Action {
	item := s.list.SelectedItem()
	if item == nil {
		return nil
	}
	resultItem := item.(*SearchResultItem)
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

	var cur *tea.Cursor
	rc := NewRenderContext(t, dialogWidth)
	rc.Title = "Search Sessions"
	if s.deleting {
		rc.TitleStyle = t.Dialog.Sessions.DeletingTitle
		rc.TitleGradientFromColor = t.Dialog.Sessions.DeletingTitleGradientFromColor
		rc.TitleGradientToColor = t.Dialog.Sessions.DeletingTitleGradientToColor
		rc.ViewStyle = t.Dialog.Sessions.DeletingView
		rc.AddPart(t.Dialog.Sessions.DeletingMessage.Render("Delete this session?"))
	} else {
		cur = s.Cursor()
		rc.AddPart(t.Dialog.InputPrompt.Render(s.input.View()))
	}
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
	s.previewRect = image.Rect(startX+leftWidth, previewStartY, startX+leftWidth+rightWidth, previewStartY+previewH)
	uv.NewStyledString(previewView).Draw(scr, s.previewRect)

	if cur != nil {
		cur.X += startX
		cur.Y += leftStartY
	}
	return cur
}

// buildPreview builds the preview panel with a rounded border.
// Each message occupies one visual line. If a line has a token match,
// it is truncated around the match so the match is visible. Lines
// without a match are truncated from the start.
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

	tokens := search.TokenizeQuery(s.previewQuery)
	innerW := max(1, borderW-2)

	// Clamp vertical scroll (one message = one line).
	maxRow := max(0, len(s.preview)-borderH)
	s.previewRow = max(0, min(s.previewRow, maxRow))

	endLine := min(len(s.preview), s.previewRow+borderH)
	visible := s.preview[s.previewRow:endLine]

	var lines []string
	for _, line := range visible {
		cut := centerTruncate(line, innerW, tokens)
		lines = append(lines, highlightLine(cut, tokens))
	}

	return border.Render(strings.Join(lines, "\n"))
}

// centerTruncate truncates a line to maxW display columns. If the line
// contains a token match, the visible window is centered on the match.
// Otherwise the line is truncated from the start.
func centerTruncate(line string, maxW int, tokens []string) string {
	lineW := ansi.StringWidth(line)
	if lineW <= maxW {
		return line
	}

	if len(tokens) == 0 {
		return ansi.Truncate(line, maxW, "…")
	}

	// Find the display-column of the first match.
	matchCol := findMatchColumn(line, tokens)
	if matchCol < 0 {
		return ansi.Truncate(line, maxW, "…")
	}

	// Center the match in the visible window.
	half := maxW / 2
	startCol := max(0, matchCol-half)

	if startCol == 0 {
		return ansi.Truncate(line, maxW, "…")
	}

	// Skip past startCol display columns.
	runes := []rune(line)
	col := 0
	startRune := 0
	for i, r := range runes {
		if col >= startCol {
			startRune = i
			break
		}
		w := ansi.StringWidth(string(r))
		col += w
	}

	tail := string(runes[startRune:])
	return "…" + ansi.Truncate(tail, max(0, maxW-1), "…")
}

// findMatchColumn returns the display column of the first token match
// in a line (direct text or per-run initials). Returns -1 if no match.
func findMatchColumn(line string, tokens []string) int {
	runes := []rune(line)
	lower := []rune(strings.ToLower(string(runes)))

	for _, token := range tokens {
		tl := []rune(strings.ToLower(token))
		for i := 0; i <= len(lower)-len(tl); i++ {
			if string(lower[i:i+len(tl)]) == string(tl) {
				return ansi.StringWidth(string(runes[:i]))
			}
		}
	}

	// Per contiguous Han run initials.
	i := 0
	for i < len(runes) {
		if !unicode.Is(unicode.Han, runes[i]) {
			i++
			continue
		}
		runStart := i
		type hanChar struct {
			runeIdx int
			initial byte
		}
		var hans []hanChar
		for i < len(runes) && unicode.Is(unicode.Han, runes[i]) {
			_, ini := search.ToPinyinAndInitials(string(runes[i]))
			if len(ini) > 0 {
				hans = append(hans, hanChar{runeIdx: i, initial: ini[0]})
			}
			i++
		}
		_ = runStart
		for _, token := range tokens {
			tl := strings.ToLower(token)
			tLen := len(tl)
			if tLen == 0 || len(hans) < tLen {
				continue
			}
			for si := 0; si <= len(hans)-tLen; si++ {
				match := true
				for j := 0; j < tLen; j++ {
					if strings.ToLower(string(hans[si+j].initial))[0] != tl[j] {
						match = false
						break
					}
				}
				if match {
					return ansi.StringWidth(string(runes[:hans[si].runeIdx]))
				}
			}
		}
	}

	return -1
}

// scrollPreviewToMatch sets previewRow to the first message matching
// the current query tokens. Called when preview data arrives.
func (s *SessionSearch) scrollPreviewToMatch() {
	tokens := search.TokenizeQuery(s.previewQuery)
	if len(tokens) == 0 {
		s.previewRow = 0
		return
	}
	for i, line := range s.preview {
		if lineMatchesAnyToken(line, tokens) {
			s.previewRow = max(0, i-2)
			return
		}
	}
	s.previewRow = 0
}

// highlightLine highlights all occurrences of tokens in a line using dark gold.
// It handles both direct text matches and pinyin matches for Chinese characters.
// All token matches are collected into a rune-level highlight mask first,
// then rendered in a single pass to avoid ANSI-offset corruption.
func highlightLine(line string, tokens []string) string {
	if len(tokens) == 0 {
		return line
	}

	runes := []rune(line)
	n := len(runes)
	if n == 0 {
		return line
	}

	hl := make([]bool, n)

	for _, token := range tokens {
		tokenLower := strings.ToLower(token)
		if tokenLower == "" {
			continue
		}
		markDirectMatches(runes, tokenLower, hl)
		markPinyinMatches(runes, tokenLower, hl)
	}

	highlightStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#DAA520")).Bold(true)

	var result strings.Builder
	i := 0
	for i < n {
		if !hl[i] {
			j := i
			for j < n && !hl[j] {
				j++
			}
			result.WriteString(string(runes[i:j]))
			i = j
		} else {
			j := i
			for j < n && hl[j] {
				j++
			}
			result.WriteString(highlightStyle.Render(string(runes[i:j])))
			i = j
		}
	}
	return result.String()
}

// markDirectMatches marks rune positions that match the token as a substring
// (case-insensitive) in the rune slice.
func markDirectMatches(runes []rune, tokenLower string, hl []bool) {
	lineLower := strings.ToLower(string(runes))
	lineRunes := []rune(lineLower)
	tokenRunes := []rune(tokenLower)
	tLen := len(tokenRunes)

	for i := 0; i <= len(lineRunes)-tLen; i++ {
		if string(lineRunes[i:i+tLen]) == tokenLower {
			for k := i; k < i+tLen; k++ {
				hl[k] = true
			}
		}
	}
}

// markPinyinMatches finds contiguous Chinese character runs, computes
// their pinyin initials, and highlights matching characters. Punctuation
// between Han characters breaks the run (matching the index behavior).
func markPinyinMatches(runes []rune, tokenLower string, hl []bool) {
	tLen := len(tokenLower)
	if tLen == 0 {
		return
	}

	i := 0
	for i < len(runes) {
		if !unicode.Is(unicode.Han, runes[i]) {
			i++
			continue
		}
		// Collect a contiguous Han run.
		start := i
		type hanChar struct {
			runeIdx int
			initial byte
		}
		var hans []hanChar
		for i < len(runes) && unicode.Is(unicode.Han, runes[i]) {
			_, ini := search.ToPinyinAndInitials(string(runes[i]))
			if len(ini) > 0 {
				hans = append(hans, hanChar{runeIdx: i, initial: ini[0]})
			}
			i++
		}
		_ = start

		// Sliding window over this run's initials.
		for si := 0; si <= len(hans)-tLen; si++ {
			match := true
			for j := 0; j < tLen; j++ {
				if strings.ToLower(string(hans[si+j].initial))[0] != tokenLower[j] {
					match = false
					break
				}
			}
			if match {
				for j := 0; j < tLen; j++ {
					hl[hans[si+j].runeIdx] = true
				}
			}
		}
	}
}

// lineMatchesAnyToken checks if a line contains any of the tokens
// via direct text or per-run pinyin initials (case-insensitive).
// Punctuation breaks Han runs, matching the FTS5 index behavior.
func lineMatchesAnyToken(line string, tokens []string) bool {
	lineLower := strings.ToLower(line)
	for _, token := range tokens {
		if strings.Contains(lineLower, strings.ToLower(token)) {
			return true
		}
	}

	// Per contiguous Han run: collect initials, check substring.
	runes := []rune(line)
	i := 0
	for i < len(runes) {
		if !unicode.Is(unicode.Han, runes[i]) {
			i++
			continue
		}
		var initials []byte
		for i < len(runes) && unicode.Is(unicode.Han, runes[i]) {
			_, ini := search.ToPinyinAndInitials(string(runes[i]))
			if len(ini) > 0 {
				initials = append(initials, ini[0])
			}
			i++
		}
		iniLower := strings.ToLower(string(initials))
		for _, token := range tokens {
			if strings.Contains(iniLower, strings.ToLower(token)) {
				return true
			}
		}
	}
	return false
}

// ShortHelp implements [help.KeyMap].
func (s *SessionSearch) ShortHelp() []key.Binding {
	if s.deleting {
		return []key.Binding{
			s.keyMap.ConfirmDelete,
			s.keyMap.AlwaysDelete,
			s.keyMap.CancelDelete,
		}
	}
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
