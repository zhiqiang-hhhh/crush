package model

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/diff"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/x/ansi"
)

// turnWindowInit is the default number of user-message turns shown when a
// session is first loaded.
const turnWindowInit = 20

// turnWindowBatch is the number of additional turns revealed when the user
// scrolls to the top.
const turnWindowBatch = 16

// messagePageSize is the number of DB messages fetched per page. This is
// larger than turnWindowInit because each turn consists of multiple messages
// (user + assistant + tool results).
const messagePageSize = 200

// loadSessionMsg is a message indicating that a session and its files have
// been loaded, including preprocessed chat messages.
type loadSessionMsg struct {
	session         *session.Session
	files           []SessionFile
	readFiles       []string
	messageItems    []chat.MessageItem
	remainingItems  []chat.MessageItem // items from the page that weren't rendered (above the turn window)
	cursor          message.MessageCursor
	hasMore         bool
	lastUserMsgTime int64
}

// lspFilePaths returns deduplicated file paths from both modified and read
// files for starting LSP servers.
func (msg loadSessionMsg) lspFilePaths() []string {
	seen := make(map[string]struct{}, len(msg.files)+len(msg.readFiles))
	paths := make([]string, 0, len(msg.files)+len(msg.readFiles))
	for _, f := range msg.files {
		p := f.LatestVersion.Path
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}
	for _, p := range msg.readFiles {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}
	return paths
}

// SessionFile tracks the first and latest versions of a file in a session,
// along with the total additions and deletions.
type SessionFile struct {
	FirstVersion  history.File
	LatestVersion history.File
	Additions     int
	Deletions     int
}

// loadSession loads the session along with its associated files and computes
// the diff statistics (additions and deletions) for each file in the session.
// It returns a tea.Cmd that, when executed, fetches the session data and
// returns a sessionFilesLoadedMsg containing the processed session files.
func (m *UI) loadSession(sessionID string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		sess, err := m.com.Workspace.GetSession(ctx, sessionID)
		if err != nil {
			return util.ReportError(err)()
		}

		sessionFiles, err := m.loadSessionFiles(sessionID)
		if err != nil {
			return util.ReportError(err)()
		}

		readFiles, err := m.com.Workspace.FileTrackerListReadFiles(ctx, sessionID)
		if err != nil {
			slog.Error("Failed to load read files for session", "error", err)
		}

		items, cursor, hasMore, lastUserMsgTime := m.prepareSessionMessages(ctx, sessionID)

		// Compute the turn window: show only the last turnWindowInit turns.
		turnStart := turnStartIndex(items, turnWindowInit)
		var windowedItems []chat.MessageItem
		if turnStart > 0 {
			windowedItems = items[turnStart:]
		} else {
			windowedItems = items
		}

		return loadSessionMsg{
			session:         &sess,
			files:           sessionFiles,
			readFiles:       readFiles,
			messageItems:    windowedItems,
			cursor:          cursor,
			hasMore:         hasMore || turnStart > 0,
			lastUserMsgTime: lastUserMsgTime,
			remainingItems:  items[:turnStart],
		}
	}
}

// prepareSessionMessages loads the most recent page of messages for a session
// off the UI thread. It returns the constructed message items, pagination info,
// and the last user message timestamp.
func (m *UI) prepareSessionMessages(ctx context.Context, sessionID string) ([]chat.MessageItem, message.MessageCursor, bool, int64) {
	page, err := m.com.App.Messages.ListRecent(ctx, sessionID, messagePageSize)
	if err != nil {
		slog.Error("Failed to load session messages", "error", err)
		return nil, message.MessageCursor{}, false, 0
	}
	msgs := page.Messages

	// Repair any assistant messages that were persisted without a Finish
	// part (e.g. due to a crash or provider error mid-stream) so the UI
	// does not show an infinite spinner.
	for i := range msgs {
		msgs[i].RepairUnfinished()
	}

	// Build tool result map.
	msgPtrs := make([]*message.Message, len(msgs))
	for i := range msgs {
		msgPtrs[i] = &msgs[i]
	}
	toolResultMap := chat.BuildToolResultMap(msgPtrs)

	var lastUserMsgTime int64
	if len(msgPtrs) > 0 {
		lastUserMsgTime = msgPtrs[0].CreatedAt
	}

	// Extract message items.
	items := make([]chat.MessageItem, 0, len(msgs)*2)
	for _, msg := range msgPtrs {
		switch msg.Role {
		case message.User:
			lastUserMsgTime = msg.CreatedAt
			items = append(items, chat.ExtractMessageItems(m.com.Styles, msg, toolResultMap)...)
		case message.Assistant:
			items = append(items, chat.ExtractMessageItems(m.com.Styles, msg, toolResultMap)...)
			if msg.FinishPart() != nil && msg.FinishPart().Reason == message.FinishReasonEndTurn {
				infoItem := chat.NewAssistantInfoItem(m.com.Styles, msg, m.com.Config(), time.Unix(lastUserMsgTime, 0))
				items = append(items, infoItem)
			}
		default:
			items = append(items, chat.ExtractMessageItems(m.com.Styles, msg, toolResultMap)...)
		}
	}

	// Load nested tool calls.
	m.loadNestedToolCalls(items)

	return items, page.Cursor, page.HasMore, lastUserMsgTime
}

// turnStartIndex scans items backwards and returns the index where the last
// maxTurns user-message turns begin. A "turn" starts at each UserMessageItem.
// If the total number of turns is <= maxTurns, it returns 0 (show everything).
func turnStartIndex(items []chat.MessageItem, maxTurns int) int {
	turns := 0
	for i := len(items) - 1; i >= 0; i-- {
		if _, ok := items[i].(*chat.UserMessageItem); ok {
			turns++
			if turns > maxTurns {
				return i
			}
		}
	}
	return 0
}

// loadMoreHistoryMsg carries the older items to prepend when the user scrolls
// to the top of the chat.
type loadMoreHistoryMsg struct {
	items          []chat.MessageItem
	remainingItems []chat.MessageItem // leftover items above the reveal window
	cursor         message.MessageCursor
	hasMore        bool
}

// loadMoreHistory returns a tea.Cmd that reveals more history. It first drains
// any remaining in-memory items (from render windowing), then fetches older
// pages from the database via cursor pagination.
func (m *UI) loadMoreHistory() tea.Cmd {
	remaining := m.remainingItems
	cursor := m.historyCursor
	hasMore := m.historyHasMore
	sessionID := ""
	if m.session != nil {
		sessionID = m.session.ID
	}
	msgSvc := m.com.App.Messages
	styles := m.com.Styles
	cfg := m.com.Config()

	return func() tea.Msg {
		// First: drain remaining in-memory items from the initial page.
		if len(remaining) > 0 {
			newStart := turnStartIndex(remaining, turnWindowBatch)
			items := remaining[newStart:]
			return loadMoreHistoryMsg{
				items:          items,
				remainingItems: remaining[:newStart],
				cursor:         cursor,
				hasMore:        hasMore || newStart > 0,
			}
		}

		// Second: fetch from DB.
		if !hasMore || sessionID == "" {
			return loadMoreHistoryMsg{}
		}

		ctx := context.Background()
		page, err := msgSvc.ListBefore(ctx, sessionID, cursor, messagePageSize)
		if err != nil {
			slog.Error("Failed to load more history", "error", err)
			return loadMoreHistoryMsg{cursor: cursor, hasMore: hasMore}
		}

		// Build items from the fetched page.
		items := buildMessageItems(styles, cfg, page.Messages)

		// Apply turn windowing to the fetched page.
		turnStart := turnStartIndex(items, turnWindowBatch)
		windowedItems := items[turnStart:]

		return loadMoreHistoryMsg{
			items:          windowedItems,
			remainingItems: items[:turnStart],
			cursor:         page.Cursor,
			hasMore:        page.HasMore || turnStart > 0,
		}
	}
}

// buildMessageItems converts a slice of messages into chat items. It does NOT
// handle nested tool calls — those are done once during
// initial load only.
func buildMessageItems(styles *styles.Styles, cfg *config.Config, msgs []message.Message) []chat.MessageItem {
	for i := range msgs {
		msgs[i].RepairUnfinished()
	}
	msgPtrs := make([]*message.Message, len(msgs))
	for i := range msgs {
		msgPtrs[i] = &msgs[i]
	}
	toolResultMap := chat.BuildToolResultMap(msgPtrs)

	var lastUserMsgTime int64
	items := make([]chat.MessageItem, 0, len(msgs)*2)
	for _, msg := range msgPtrs {
		switch msg.Role {
		case message.User:
			lastUserMsgTime = msg.CreatedAt
			items = append(items, chat.ExtractMessageItems(styles, msg, toolResultMap)...)
		case message.Assistant:
			items = append(items, chat.ExtractMessageItems(styles, msg, toolResultMap)...)
			if msg.FinishPart() != nil && msg.FinishPart().Reason == message.FinishReasonEndTurn {
				infoItem := chat.NewAssistantInfoItem(styles, msg, cfg, time.Unix(lastUserMsgTime, 0))
				items = append(items, infoItem)
			}
		default:
			items = append(items, chat.ExtractMessageItems(styles, msg, toolResultMap)...)
		}
	}
	return items
}

func (m *UI) loadSessionFiles(sessionID string) ([]SessionFile, error) {
	files, err := m.com.Workspace.ListSessionHistory(context.Background(), sessionID)
	if err != nil {
		return nil, err
	}

	filesByPath := make(map[string][]history.File)
	for _, f := range files {
		filesByPath[f.Path] = append(filesByPath[f.Path], f)
	}
	sessionFiles := make([]SessionFile, 0, len(filesByPath))
	for _, versions := range filesByPath {
		if len(versions) == 0 {
			continue
		}

		first := versions[0]
		last := versions[0]
		for _, v := range versions {
			if v.Version < first.Version {
				first = v
			}
			if v.Version > last.Version {
				last = v
			}
		}

		_, additions, deletions := diff.GenerateDiff(first.Content, last.Content, first.Path)

		sessionFiles = append(sessionFiles, SessionFile{
			FirstVersion:  first,
			LatestVersion: last,
			Additions:     additions,
			Deletions:     deletions,
		})
	}

	slices.SortFunc(sessionFiles, func(a, b SessionFile) int {
		if a.LatestVersion.UpdatedAt > b.LatestVersion.UpdatedAt {
			return -1
		}
		if a.LatestVersion.UpdatedAt < b.LatestVersion.UpdatedAt {
			return 1
		}
		return 0
	})
	return sessionFiles, nil
}

// handleFileEvent processes file change events and updates the session file
// list with new or updated file information.
func (m *UI) handleFileEvent(file history.File) tea.Cmd {
	if m.session == nil || file.SessionID != m.session.ID {
		return nil
	}

	sessionID := m.session.ID
	return func() tea.Msg {
		sessionFiles, err := m.loadSessionFiles(sessionID)
		// could not load session files
		if err != nil {
			return util.NewErrorMsg(err)
		}

		return sessionFilesUpdatesMsg{
			sessionFiles: sessionFiles,
		}
	}
}

// filesInfo renders the modified files section for the sidebar, showing files
// with their addition/deletion counts.
func (m *UI) filesInfo(cwd string, width, maxItems int, isSection bool) string {
	t := m.com.Styles

	title := t.Subtle.Render("Modified Files")
	if isSection {
		title = common.Section(t, "Modified Files", width)
	}
	list := t.Subtle.Render("None")
	var filesWithChanges []SessionFile
	for _, f := range m.sessionFiles {
		if f.Additions == 0 && f.Deletions == 0 {
			continue
		}
		filesWithChanges = append(filesWithChanges, f)
	}
	if len(filesWithChanges) > 0 {
		list = fileList(t, cwd, filesWithChanges, width, maxItems)
	}

	return lipgloss.NewStyle().Width(width).Render(fmt.Sprintf("%s\n\n%s", title, list))
}

// fileList renders a list of files with their diff statistics, truncating to
// maxItems and showing a "...and N more" message if needed.
func fileList(t *styles.Styles, cwd string, filesWithChanges []SessionFile, width, maxItems int) string {
	if maxItems <= 0 {
		return ""
	}
	var renderedFiles []string
	filesShown := 0

	for _, f := range filesWithChanges {
		// Skip files with no changes
		if filesShown >= maxItems {
			break
		}

		// Build stats string with colors
		var statusParts []string
		if f.Additions > 0 {
			statusParts = append(statusParts, t.Files.Additions.Render(fmt.Sprintf("+%d", f.Additions)))
		}
		if f.Deletions > 0 {
			statusParts = append(statusParts, t.Files.Deletions.Render(fmt.Sprintf("-%d", f.Deletions)))
		}
		extraContent := strings.Join(statusParts, " ")

		// Format file path
		filePath := f.FirstVersion.Path
		if rel, err := filepath.Rel(cwd, filePath); err == nil {
			filePath = rel
		}
		filePath = fsext.DirTrim(filePath, 2)
		filePath = ansi.Truncate(filePath, width-(lipgloss.Width(extraContent)-2), "…")

		line := t.Files.Path.Render(filePath)
		if extraContent != "" {
			line = fmt.Sprintf("%s %s", line, extraContent)
		}

		renderedFiles = append(renderedFiles, line)
		filesShown++
	}

	if len(filesWithChanges) > maxItems {
		remaining := len(filesWithChanges) - maxItems
		renderedFiles = append(renderedFiles, t.Subtle.Render(fmt.Sprintf("…and %d more", remaining)))
	}

	return lipgloss.JoinVertical(lipgloss.Left, renderedFiles...)
}

// startLSPs starts LSP servers for the given file paths.
func (m *UI) startLSPs(paths []string) tea.Cmd {
	if len(paths) == 0 {
		return nil
	}

	return func() tea.Msg {
		ctx := context.Background()
		for _, path := range paths {
			m.com.Workspace.LSPStart(ctx, path)
		}
		return nil
	}
}
