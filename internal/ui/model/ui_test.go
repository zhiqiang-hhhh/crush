package model

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/attachments"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

func TestCurrentModelSupportsImages(t *testing.T) {
	t.Parallel()

	t.Run("returns false when config is nil", func(t *testing.T) {
		t.Parallel()

		ui := newTestUIWithConfig(t, nil)
		require.False(t, ui.currentModelSupportsImages())
	})

	t.Run("returns false when coder agent is missing", func(t *testing.T) {
		t.Parallel()

		cfg := &config.Config{
			Providers: csync.NewMap[string, config.ProviderConfig](),
			Agents:    map[string]config.Agent{},
		}
		ui := newTestUIWithConfig(t, cfg)
		require.False(t, ui.currentModelSupportsImages())
	})

	t.Run("returns false when model is not found", func(t *testing.T) {
		t.Parallel()

		cfg := &config.Config{
			Providers: csync.NewMap[string, config.ProviderConfig](),
			Agents: map[string]config.Agent{
				config.AgentCoder: {Model: config.SelectedModelTypeLarge},
			},
		}
		ui := newTestUIWithConfig(t, cfg)
		require.False(t, ui.currentModelSupportsImages())
	})

	t.Run("returns true when current model supports images", func(t *testing.T) {
		t.Parallel()

		providers := csync.NewMap[string, config.ProviderConfig]()
		providers.Set("test-provider", config.ProviderConfig{
			ID: "test-provider",
			Models: []catwalk.Model{
				{ID: "test-model", SupportsImages: true},
			},
		})

		cfg := &config.Config{
			Models: map[config.SelectedModelType]config.SelectedModel{
				config.SelectedModelTypeLarge: {
					Provider: "test-provider",
					Model:    "test-model",
				},
			},
			Providers: providers,
			Agents: map[string]config.Agent{
				config.AgentCoder: {Model: config.SelectedModelTypeLarge},
			},
		}

		ui := newTestUIWithConfig(t, cfg)
		require.True(t, ui.currentModelSupportsImages())
	})
}

func TestHandleKeyPressMsg_NewSessionAllowedWhileAnotherSessionBusy(t *testing.T) {
	t.Parallel()

	ui := newBusySessionTestUI(t)
	cmd := ui.handleKeyPressMsg(tea.KeyPressMsg{Text: "ctrl+n"})

	require.NotNil(t, cmd)
	require.Nil(t, ui.session)
	require.Equal(t, uiFocusEditor, ui.focus)
}

func TestHandleDialogMsg_NewSessionAllowedWhileAnotherSessionBusy(t *testing.T) {
	t.Parallel()

	ui := newBusySessionTestUI(t)
	ui.dialog.OpenDialog(staticActionDialog{id: dialog.CommandsID, action: dialog.ActionNewSession{}})

	cmd := ui.handleDialogMsg(tea.KeyPressMsg{})

	require.NotNil(t, cmd)
	require.Nil(t, ui.session)
	require.False(t, ui.dialog.HasDialogs())
	require.Equal(t, util.InfoMsg{}, ui.status.msg)
}

func newTestUIWithConfig(t *testing.T, cfg *config.Config) *UI {
	t.Helper()

	return &UI{
		com: &common.Common{
			Workspace: &testWorkspace{cfg: cfg},
		},
	}
}

func newBusySessionTestUI(t *testing.T) *UI {
	t.Helper()

	ws := &testWorkspace{agentReady: true, agentBusy: true}
	com := common.DefaultCommon(ws)

	ta := textarea.New()
	ta.SetStyles(com.Styles.TextArea)
	ta.ShowLineNumbers = false
	ta.CharLimit = -1
	ta.SetVirtualCursor(false)
	ta.DynamicHeight = true
	ta.MinHeight = TextareaMinHeight
	ta.MaxHeight = TextareaMaxHeight
	ta.Focus()

	keyMap := DefaultKeyMap()
	ui := &UI{
		com:      com,
		session:  &session.Session{ID: "session-1", Title: "Session 1"},
		dialog:   dialog.NewOverlay(),
		status:   NewStatus(com, nil),
		chat:     NewChat(com),
		textarea: ta,
		attachments: attachments.New(
			attachments.NewRenderer(
				com.Styles.Attachments.Normal,
				com.Styles.Attachments.Deleting,
				com.Styles.Attachments.Image,
				com.Styles.Attachments.Text,
			),
			attachments.Keymap{
				DeleteMode: keyMap.Editor.AttachmentDeleteMode,
				DeleteAll:  keyMap.Editor.DeleteAllAttachments,
				Escape:     keyMap.Editor.Escape,
			},
		),
		state:              uiChat,
		focus:              uiFocusEditor,
		keyMap:             keyMap,
		width:              140,
		height:             45,
		pendingToolResults: make(map[string]*message.ToolResult),
	}
	ui.updateLayoutAndSize()
	return ui
}

// testWorkspace is a minimal [workspace.Workspace] stub for unit tests.
type testWorkspace struct {
	workspace.Workspace
	cfg        *config.Config
	agentReady bool
	agentBusy  bool
}

func (w *testWorkspace) Config() *config.Config {
	return w.cfg
}

func (w *testWorkspace) AgentIsReady() bool {
	return w.agentReady
}

func (w *testWorkspace) AgentIsBusy() bool {
	return w.agentBusy
}

type staticActionDialog struct {
	id     string
	action dialog.Action
}

func (d staticActionDialog) ID() string {
	return d.id
}

func (d staticActionDialog) HandleMsg(tea.Msg) dialog.Action {
	return d.action
}

func (d staticActionDialog) Draw(uv.Screen, uv.Rectangle) *tea.Cursor {
	return nil
}
