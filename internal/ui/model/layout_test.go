package model

import (
	"strconv"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/common"
)

// testMessageItem is a minimal chat item used to populate the chat list
// without pulling in full message rendering machinery.
type testMessageItem struct {
	id   string
	text string
}

func (m testMessageItem) ID() string           { return m.id }
func (m testMessageItem) Render(int) string    { return m.text }
func (m testMessageItem) RawRender(int) string { return m.text }

var _ chat.MessageItem = testMessageItem{}

// newTestUI builds a focused uiChat model with dynamic textarea sizing enabled.
// It intentionally keeps dependencies minimal so layout behavior can be tested
// in isolation.
func newTestUI() *UI {
	com := common.DefaultCommon(nil)

	ta := textarea.New()
	ta.SetStyles(com.Styles.TextArea)
	ta.ShowLineNumbers = false
	ta.CharLimit = -1
	ta.SetVirtualCursor(false)
	ta.DynamicHeight = true
	ta.MinHeight = TextareaMinHeight
	ta.MaxHeight = TextareaMaxHeight
	ta.Focus()

	u := &UI{
		com:      com,
		status:   NewStatus(com, nil),
		chat:     NewChat(com),
		textarea: ta,
		state:    uiChat,
		focus:    uiFocusEditor,
		width:    140,
		height:   45,
	}

	return u
}

func TestUpdateLayoutAndSize_EditorGrowthShrinksChat(t *testing.T) {
	t.Parallel()

	// Baseline layout at min textarea height.
	u := newTestUI()
	u.updateLayoutAndSize()

	initialEditorHeight := u.layout.editor.Dy()
	initialChatHeight := u.layout.main.Dy()

	// Increase textarea content enough to trigger growth, then run the
	// same resize hook used in the real update path.
	prevHeight := u.textarea.Height()
	u.textarea.SetValue(strings.Repeat("line\n", 8))
	u.textarea.MoveToEnd()
	_ = u.handleTextareaHeightChange(prevHeight)

	if got := u.layout.editor.Dy(); got <= initialEditorHeight {
		t.Fatalf("expected editor to grow: got %d, want > %d", got, initialEditorHeight)
	}

	if got := u.layout.main.Dy(); got >= initialChatHeight {
		t.Fatalf("expected chat to shrink: got %d, want < %d", got, initialChatHeight)
	}
}

func TestScrollBy_ReEngagesFollowAtBottom(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.updateLayoutAndSize()

	msgs := make([]chat.MessageItem, 0, 60)
	for i := range 60 {
		msgs = append(msgs, testMessageItem{
			id:   "m-" + strconv.Itoa(i),
			text: "message " + strconv.Itoa(i),
		})
	}
	u.chat.SetMessages(msgs...)

	// After SetMessages, follow should be true and at bottom.
	if !u.chat.Follow() {
		t.Fatal("expected follow mode after SetMessages")
	}
	if !u.chat.AtBottom() {
		t.Fatal("expected at bottom after SetMessages")
	}

	// Scroll up — follow should disengage.
	u.chat.ScrollBy(-10)
	if u.chat.Follow() {
		t.Fatal("expected follow mode to be disabled after scrolling up")
	}
	if u.chat.AtBottom() {
		t.Fatal("expected not to be at bottom after scrolling up")
	}

	// Scroll back down to the bottom — follow should re-engage.
	u.chat.ScrollBy(999)
	if !u.chat.AtBottom() {
		t.Fatal("expected at bottom after scrolling down past end")
	}
	if !u.chat.Follow() {
		t.Fatal("expected follow mode to re-engage when scrolled to bottom")
	}
}

func TestScrollBy_ReEngagesFollowAfterContentGrows(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.updateLayoutAndSize()

	msgs := make([]chat.MessageItem, 0, 60)
	for i := range 60 {
		msgs = append(msgs, testMessageItem{
			id:   "m-" + strconv.Itoa(i),
			text: "message " + strconv.Itoa(i),
		})
	}
	u.chat.SetMessages(msgs...)

	// Scroll up — follow should disengage.
	u.chat.ScrollBy(-20)
	if u.chat.Follow() {
		t.Fatal("expected follow mode to be disabled after scrolling up")
	}

	// Simulate streaming: append new messages while user is scrolled up.
	for i := 60; i < 80; i++ {
		u.chat.AppendMessages(testMessageItem{
			id:   "m-" + strconv.Itoa(i),
			text: "new message " + strconv.Itoa(i),
		})
	}

	// Still should not be following (user is scrolled up).
	if u.chat.Follow() {
		t.Fatal("expected follow mode to remain disabled while scrolled up")
	}

	// Scroll down to the very bottom.
	u.chat.ScrollBy(9999)
	if !u.chat.AtBottom() {
		t.Fatal("expected at bottom after large scroll down")
	}
	if !u.chat.Follow() {
		t.Fatal("expected follow mode to re-engage after scrolling to bottom")
	}
}

func TestScrollBy_ReEngagesFollowNearBottom(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.updateLayoutAndSize()

	// Use multi-line messages so the list is significantly scrollable.
	msgs := make([]chat.MessageItem, 0, 40)
	for i := range 40 {
		msgs = append(msgs, testMessageItem{
			id:   "m-" + strconv.Itoa(i),
			text: "message line 1\nmessage line 2\nmessage line 3",
		})
	}
	u.chat.SetMessages(msgs...)

	// Scroll up far — follow should disengage.
	u.chat.ScrollBy(-200)
	if u.chat.Follow() {
		t.Fatal("expected follow mode to be disabled after scrolling up")
	}

	// Scroll down to near the bottom but not exactly at it.
	// First scroll most of the way with a large value, then back off slightly.
	u.chat.ScrollBy(9999)
	if !u.chat.Follow() {
		t.Fatal("expected follow mode at bottom")
	}

	// Now scroll up just a few lines (within the near-bottom threshold).
	u.chat.ScrollBy(-3)
	// After scrolling up, follow is disabled.
	if u.chat.Follow() {
		t.Fatal("expected follow mode disabled after small scroll up")
	}

	// Scroll down by 1 line — we should be near bottom and follow re-engages.
	u.chat.ScrollBy(1)
	if !u.chat.NearBottom() {
		t.Fatal("expected to be near bottom after scrolling down by 1")
	}
	if !u.chat.Follow() {
		t.Fatal("expected follow mode to re-engage when near bottom")
	}
}

func TestHandleTextareaHeightChange_FollowModeStaysAtBottom(t *testing.T) {
	t.Parallel()

	// Use enough messages to make the chat scrollable so AtBottom/Follow
	// assertions are meaningful.
	u := newTestUI()

	msgs := make([]chat.MessageItem, 0, 60)
	for i := range 60 {
		msgs = append(msgs, testMessageItem{
			id:   "m-" + strconv.Itoa(i),
			text: "message " + strconv.Itoa(i),
		})
	}
	u.chat.SetMessages(msgs...)
	u.updateLayoutAndSize()

	// Enter follow mode and verify we're anchored at the bottom first.
	u.chat.ScrollToBottom()
	if !u.chat.AtBottom() {
		t.Fatal("expected chat to start at bottom")
	}

	// Grow the editor; follow mode should keep the chat pinned to the end
	// even as the chat viewport shrinks.
	prevHeight := u.textarea.Height()
	u.textarea.SetValue(strings.Repeat("line\n", 10))
	u.textarea.MoveToEnd()
	_ = u.handleTextareaHeightChange(prevHeight)

	if !u.chat.Follow() {
		t.Fatal("expected follow mode to remain enabled")
	}
	if !u.chat.AtBottom() {
		t.Fatal("expected chat to remain at bottom after editor resize in follow mode")
	}
}
