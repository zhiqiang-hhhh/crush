package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/projects"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestTmuxSocketPathIsPerDirectory(t *testing.T) {
	t.Parallel()

	a := tmuxSocketPath("/tmp/project-a")
	b := tmuxSocketPath("/tmp/project-b")
	c := tmuxSocketPath("/tmp/project-a")

	require.NotEqual(t, a, b)
	require.Equal(t, a, c)
}

func TestSamePath(t *testing.T) {
	t.Parallel()

	require.True(t, samePath("/tmp/foo", "/tmp/foo"))
	require.True(t, samePath("/tmp/foo/", "/tmp/foo"))
	require.False(t, samePath("/tmp/foo", "/tmp/bar"))
}

func TestHasExplicitStartupTarget(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("cwd", "", "")
	cmd.Flags().String("data-dir", "", "")
	cmd.Flags().String("session", "", "")
	cmd.Flags().Bool("continue", false, "")

	require.False(t, hasExplicitStartupTarget(cmd))
	require.NoError(t, cmd.Flags().Set("cwd", "/tmp/project"))
	require.True(t, hasExplicitStartupTarget(cmd))
}

func TestPrepareInteractiveStartup(t *testing.T) {
	t.Run("home without explicit target uses global last session", func(t *testing.T) {
		homeDir := t.TempDir()
		otherProject := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		t.Setenv("CRUSH_GLOBAL_DATA", filepath.Join(t.TempDir(), "crush"))
		require.NoError(t, projects.Register(homeDir, filepath.Join(homeDir, ".crush")))
		require.NoError(t, projects.Register(otherProject, filepath.Join(otherProject, ".crush")))

		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().String("cwd", "", "")
		cmd.Flags().String("data-dir", "", "")
		cmd.Flags().String("session", "", "")
		cmd.Flags().Bool("continue", false, "")
		t.Cleanup(func() {
			_ = os.Chdir("/")
		})
		require.NoError(t, os.Chdir(homeDir))

		dataDir := filepath.Join(otherProject, ".crush")
		require.NoError(t, os.MkdirAll(dataDir, 0o700))
		conn, err := db.Connect(t.Context(), dataDir)
		require.NoError(t, err)
		defer conn.Close()
		svc := session.NewService(db.New(conn), conn)
		created, err := svc.Create(t.Context(), "latest")
		require.NoError(t, err)

		sessionID, continueLast, err := prepareInteractiveStartup(cmd, "", false)
		require.NoError(t, err)
		require.Equal(t, created.ID, sessionID)
		require.False(t, continueLast)
		cwd, _ := cmd.Flags().GetString("cwd")
		require.Equal(t, otherProject, cwd)
	})

	t.Run("home without explicit target errors when no global session", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		t.Setenv("CRUSH_GLOBAL_DATA", filepath.Join(t.TempDir(), "crush"))
		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().String("cwd", "", "")
		cmd.Flags().String("data-dir", "", "")
		cmd.Flags().String("session", "", "")
		cmd.Flags().Bool("continue", false, "")
		t.Cleanup(func() {
			_ = os.Chdir("/")
		})
		require.NoError(t, os.Chdir(homeDir))

		_, _, err := prepareInteractiveStartup(cmd, "", false)
		require.Error(t, err)
		require.Contains(t, err.Error(), "home directory cannot be used as a crush project")
	})

	t.Run("explicit target bypasses home restriction", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().String("cwd", "", "")
		cmd.Flags().String("data-dir", "", "")
		cmd.Flags().String("session", "", "")
		cmd.Flags().Bool("continue", false, "")
		require.NoError(t, cmd.Flags().Set("cwd", homeDir))

		sessionID, continueLast, err := prepareInteractiveStartup(cmd, "", false)
		require.NoError(t, err)
		require.Empty(t, sessionID)
		require.False(t, continueLast)
	})
}

func TestShouldContinueMostRecentLocalSession(t *testing.T) {
	t.Parallel()

	t.Run("has sessions", func(t *testing.T) {
		t.Parallel()

		continueLast, err := shouldContinueMostRecentLocalSession(t.Context(), func(context.Context) ([]session.Session, error) {
			return []session.Session{{ID: "existing"}}, nil
		})
		require.NoError(t, err)
		require.True(t, continueLast)
	})

	t.Run("no sessions", func(t *testing.T) {
		t.Parallel()

		continueLast, err := shouldContinueMostRecentLocalSession(t.Context(), func(context.Context) ([]session.Session, error) {
			return nil, nil
		})
		require.NoError(t, err)
		require.False(t, continueLast)
	})
}

func TestRequestedCwd(t *testing.T) {
	t.Parallel()

	t.Run("uses explicit cwd flag", func(t *testing.T) {
		t.Parallel()

		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().String("cwd", "", "")
		require.NoError(t, cmd.Flags().Set("cwd", "/tmp/doris"))

		cwd, err := requestedCwd(cmd, func() (string, error) {
			return "/tmp/ignored", nil
		})
		require.NoError(t, err)
		require.Equal(t, "/tmp/doris", cwd)
	})

	t.Run("falls back to getwd", func(t *testing.T) {
		t.Parallel()

		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().String("cwd", "", "")

		cwd, err := requestedCwd(cmd, func() (string, error) {
			return "/tmp/fallback", nil
		})
		require.NoError(t, err)
		require.Equal(t, "/tmp/fallback", cwd)
	})
}

func TestBuildInnerTmuxArgs(t *testing.T) {
	t.Parallel()

	newCmd := func() *cobra.Command {
		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().Bool("yolo", false, "")
		cmd.Flags().String("cwd", "", "")
		cmd.Flags().String("data-dir", "", "")
		cmd.Flags().Bool("debug", false, "")
		cmd.Flags().String("session", "", "")
		cmd.Flags().Bool("continue", false, "")
		return cmd
	}

	t.Run("preserves explicit continue", func(t *testing.T) {
		t.Parallel()

		cmd := newCmd()
		require.NoError(t, cmd.Flags().Set("continue", "true"))

		require.Equal(t, []string{"--continue"}, buildInnerTmuxArgs(cmd))
	})

	t.Run("does not force continue by default", func(t *testing.T) {
		t.Parallel()

		cmd := newCmd()

		require.Empty(t, buildInnerTmuxArgs(cmd))
	})

	t.Run("prefers session over continue", func(t *testing.T) {
		t.Parallel()

		cmd := newCmd()
		require.NoError(t, cmd.Flags().Set("continue", "true"))
		require.NoError(t, cmd.Flags().Set("session", "sess-123"))

		require.Equal(t, []string{"--session", "sess-123"}, buildInnerTmuxArgs(cmd))
	})

	t.Run("preserves cwd in tmux args", func(t *testing.T) {
		t.Parallel()

		cmd := newCmd()
		require.NoError(t, cmd.Flags().Set("cwd", "/tmp/doris"))

		require.Equal(t, []string{"--cwd", "/tmp/doris"}, buildInnerTmuxArgs(cmd))
	})
}
