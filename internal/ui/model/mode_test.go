package model

import (
	"testing"

	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

func TestParseModeCommand(t *testing.T) {
	t.Parallel()

	t.Run("show current mode", func(t *testing.T) {
		cmd, err, ok := parseModeCommand("/mode")
		require.True(t, ok)
		require.NoError(t, err)
		require.True(t, cmd.showCurrent)
	})

	t.Run("switch to plan", func(t *testing.T) {
		cmd, err, ok := parseModeCommand("/mode plan")
		require.True(t, ok)
		require.NoError(t, err)
		require.Equal(t, session.SessionModePlan, cmd.mode)
	})

	t.Run("plan alias", func(t *testing.T) {
		cmd, err, ok := parseModeCommand("/plan")
		require.True(t, ok)
		require.NoError(t, err)
		require.Equal(t, session.SessionModePlan, cmd.mode)
	})

	t.Run("invalid mode value", func(t *testing.T) {
		_, err, ok := parseModeCommand("/mode nope")
		require.True(t, ok)
		require.Error(t, err)
	})

	t.Run("non mode command", func(t *testing.T) {
		_, err, ok := parseModeCommand("hello")
		require.False(t, ok)
		require.NoError(t, err)
	})
}

func TestModeAgentID(t *testing.T) {
	t.Parallel()

	require.Equal(t, "plan", modeAgentID(session.SessionModePlan))
	require.Equal(t, "coder", modeAgentID(session.SessionModeBuild))
	require.Equal(t, "coder", modeAgentID("unknown"))
}
