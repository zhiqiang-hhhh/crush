package prompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPromptData_LoadsGlobalContextFiles(t *testing.T) {

	globalDir := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("CRUSH_GLOBAL_CONFIG", globalDir)

	os.WriteFile(filepath.Join(globalDir, "AGENTS.md"), []byte("global instructions"), 0o644)

	cfg := &config.Config{Options: &config.Options{
		DataDirectory: filepath.Join(workingDir, ".crush"),
	}}
	store := config.NewConfigStoreForTesting(cfg, workingDir)
	p, err := NewPrompt("test", "{{range .ContextFiles}}[{{.Path}}:{{.Content}}]{{end}}")
	require.NoError(t, err)

	data, err := p.promptData(context.Background(), "test", "test-model", store)
	require.NoError(t, err)

	found := false
	for _, cf := range data.ContextFiles {
		if strings.Contains(cf.Content, "global instructions") {
			found = true
			break
		}
	}
	require.True(t, found, "global AGENTS.md should be loaded")
}

func TestPromptData_BothGlobalAndProjectLoaded(t *testing.T) {

	globalDir := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("CRUSH_GLOBAL_CONFIG", globalDir)

	os.WriteFile(filepath.Join(globalDir, "AGENTS.md"), []byte("global instructions"), 0o644)
	os.WriteFile(filepath.Join(workingDir, "AGENTS.md"), []byte("project instructions"), 0o644)

	cfg := &config.Config{Options: &config.Options{
		DataDirectory: filepath.Join(workingDir, ".crush"),
	}}
	store := config.NewConfigStoreForTesting(cfg, workingDir)
	p, err := NewPrompt("test", "{{range .ContextFiles}}[{{.Content}}]{{end}}")
	require.NoError(t, err)

	data, err := p.promptData(context.Background(), "test", "test-model", store)
	require.NoError(t, err)

	globalFound := false
	projectFound := false
	for _, cf := range data.ContextFiles {
		if strings.Contains(cf.Content, "global instructions") {
			globalFound = true
		}
		if strings.Contains(cf.Content, "project instructions") {
			projectFound = true
		}
	}
	require.True(t, projectFound, "project AGENTS.md should be loaded")
	require.True(t, globalFound, "global AGENTS.md should also be loaded")
}

func TestPromptData_GlobalDirNotExist(t *testing.T) {

	t.Setenv("CRUSH_GLOBAL_CONFIG", "/nonexistent/path/to/config")
	workingDir := t.TempDir()

	cfg := &config.Config{Options: &config.Options{
		DataDirectory: filepath.Join(workingDir, ".crush"),
	}}
	store := config.NewConfigStoreForTesting(cfg, workingDir)
	p, err := NewPrompt("test", "{{range .ContextFiles}}[{{.Content}}]{{end}}")
	require.NoError(t, err)

	data, err := p.promptData(context.Background(), "test", "test-model", store)
	require.NoError(t, err)
	require.Empty(t, data.ContextFiles)
}

func TestPromptData_GlobalInContextPathsNoDuplicate(t *testing.T) {
	globalDir := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv("CRUSH_GLOBAL_CONFIG", globalDir)

	os.WriteFile(filepath.Join(globalDir, "AGENTS.md"), []byte("global instructions"), 0o644)
	os.WriteFile(filepath.Join(workingDir, "AGENTS.md"), []byte("project instructions"), 0o644)

	cfg := &config.Config{Options: &config.Options{
		DataDirectory: filepath.Join(workingDir, ".crush"),
	}}
	store := config.NewConfigStoreForTesting(cfg, workingDir)

	// Append the absolute global path after setDefaults. This simulates a
	// user adding "~/.config/crush/AGENTS.md" to context_paths. The global
	// file should appear exactly once (already loaded in the global phase),
	// not duplicated.
	cfg.Options.ContextPaths = append(cfg.Options.ContextPaths, filepath.Join(globalDir, "AGENTS.md"))

	p, err := NewPrompt("test", "{{range .ContextFiles}}[{{.Content}}]{{end}}")
	require.NoError(t, err)

	data, err := p.promptData(context.Background(), "test", "test-model", store)
	require.NoError(t, err)

	projectCount := 0
	globalCount := 0
	for _, cf := range data.ContextFiles {
		if strings.Contains(cf.Content, "project instructions") {
			projectCount++
		}
		if strings.Contains(cf.Content, "global instructions") {
			globalCount++
		}
	}
	require.Equal(t, 1, projectCount, "project AGENTS.md should appear exactly once")
	require.Equal(t, 1, globalCount, "global AGENTS.md should appear exactly once (not duplicated)")
}
