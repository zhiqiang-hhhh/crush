// Package trace provides a lightweight global trace recorder for debugging
// Crush behavior at runtime. Users toggle tracing via the /trace command;
// while active, key agent lifecycle events are collected in memory. When
// tracing stops, the collected events are returned for analysis.
package trace

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// Event is a single trace record.
type Event struct {
	Time      string         `json:"time"`
	Category  string         `json:"category"`
	Event     string         `json:"event"`
	SessionID string         `json:"session_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

var (
	mu     sync.Mutex
	active bool
	events []Event
)

// Start begins recording trace events in memory.
func Start() {
	mu.Lock()
	defer mu.Unlock()

	active = true
	events = []Event{{
		Time:     time.Now().UTC().Format(time.RFC3339Nano),
		Category: "trace",
		Event:    "started",
	}}
}

// Stop ends recording and returns all collected events as a JSONL string.
// Returns empty string if tracing was not active.
func Stop() string {
	mu.Lock()
	defer mu.Unlock()

	if !active {
		return ""
	}

	events = append(events, Event{
		Time:     time.Now().UTC().Format(time.RFC3339Nano),
		Category: "trace",
		Event:    "stopped",
	})

	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	for _, e := range events {
		_ = enc.Encode(e)
	}

	result := sb.String()
	active = false
	events = nil
	return result
}

// IsActive reports whether tracing is currently active.
func IsActive() bool {
	mu.Lock()
	defer mu.Unlock()
	return active
}

// Emit records a trace event. No-op when tracing is inactive.
func Emit(category, event, sessionID string, data map[string]any) {
	mu.Lock()
	defer mu.Unlock()

	if !active {
		return
	}

	events = append(events, Event{
		Time:      time.Now().UTC().Format(time.RFC3339Nano),
		Category:  category,
		Event:     event,
		SessionID: sessionID,
		Data:      data,
	})
}
