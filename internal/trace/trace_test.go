package trace

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartStopRoundTrip(t *testing.T) {
	Start()
	assert.True(t, IsActive())

	Emit("agent", "run_start", "sess-1", map[string]any{"model": "test"})
	Emit("tool", "call", "sess-1", map[string]any{"tool_name": "bash"})

	result := Stop()
	assert.False(t, IsActive())
	assert.NotEmpty(t, result)

	lines := strings.Split(strings.TrimSpace(result), "\n")
	require.Equal(t, 4, len(lines))

	var ev Event
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &ev))
	assert.Equal(t, "trace", ev.Category)
	assert.Equal(t, "started", ev.Event)

	require.NoError(t, json.Unmarshal([]byte(lines[1]), &ev))
	assert.Equal(t, "agent", ev.Category)
	assert.Equal(t, "run_start", ev.Event)
	assert.Equal(t, "sess-1", ev.SessionID)

	require.NoError(t, json.Unmarshal([]byte(lines[3]), &ev))
	assert.Equal(t, "stopped", ev.Event)
}

func TestStopWhenInactive(t *testing.T) {
	result := Stop()
	assert.Empty(t, result)
}

func TestEmitWhenInactive(t *testing.T) {
	Emit("agent", "test", "", nil)
	assert.False(t, IsActive())
}

func TestDoubleStart(t *testing.T) {
	Start()
	Emit("agent", "first", "", nil)

	Start()
	Emit("agent", "second", "", nil)

	result := Stop()
	lines := strings.Split(strings.TrimSpace(result), "\n")
	require.Equal(t, 3, len(lines))
}
