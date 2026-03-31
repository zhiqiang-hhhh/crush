package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatShellOutput(t *testing.T) {
	t.Parallel()

	t.Run("stdout only", func(t *testing.T) {
		output := formatShellOutput("pwd", "/tmp\n", "", nil)
		require.Contains(t, output, "$ pwd")
		require.Contains(t, output, "/tmp")
		require.Contains(t, output, "Exit code: 0")
	})

	t.Run("stderr and error", func(t *testing.T) {
		output := formatShellOutput("bad", "", "boom\n", errors.New("exit status 1"))
		require.Contains(t, output, "stderr:\nboom")
		require.Contains(t, output, "Exit code: 1")
		require.Contains(t, output, "Error: exit status 1")
	})

	t.Run("no output", func(t *testing.T) {
		output := formatShellOutput("true", "", "", nil)
		require.Contains(t, output, "(no output)")
	})
}
