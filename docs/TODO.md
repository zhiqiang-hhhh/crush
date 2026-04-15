# TODO: Fix Terminal Freeze on macOS During Streaming

## Problem

In coder mode, when the LLM streams a long response (e.g. multi-file code
generation), the entire terminal (iTerm2) freezes. Ctrl+C does not work;
the user must force-close the terminal window. Planner mode is unaffected
because its responses are short.

## Root Cause

Every LLM token triggers a full glamour markdown re-render of the entire
accumulated response, with no rate limiting. This creates an O(n²) render
storm that starves Bubble Tea's event loop, causing a terminal I/O deadlock.

The full chain:

1. Each token delta calls `streamingWriter.Update()`, which immediately
   publishes a pubsub event (`streaming_writer.go:38`) — no throttling.
2. The event reaches the TUI, calling `SetMessage()` → `clearCache()`
   (`assistant.go:257`), invalidating the render cache.
3. The next `View()` frame calls `glamour.Render()` on the **entire**
   accumulated message content — not just the new delta.
4. As the response grows, each render takes longer, but tokens keep arriving
   at 50–100+/s. The event loop falls behind.
5. Bubble Tea is in raw mode (ISIG disabled), so Ctrl+C is just another key
   event that requires the event loop to process — but the loop is blocked
   in rendering.
6. `MouseModeCellMotion` (`ui.go:2533`) is always on. The terminal keeps
   sending mouse events to stdin while the app is busy rendering and not
   reading. The PTY stdin buffer fills up.
7. iTerm2's write to PTY blocks → iTerm2's own render pipeline stalls →
   **entire terminal window freezes**.

## Why macOS Is Worse Than Linux

The bug exists on all platforms, but macOS + iTerm2 hits the deadlock faster:

- **iTerm2 I/O model**: PTY write and terminal rendering are coupled; a
  blocked PTY write freezes the whole window. Linux terminals (Alacritty,
  kitty) use separate threads for PTY I/O and GPU rendering, so a blocked
  write doesn't freeze input handling.
- **Trackpad mouse events**: macOS trackpads have a much higher sampling
  rate than Linux mice/touchpads, flooding more mouse escape sequences into
  the PTY stdin buffer.
- **Smaller PTY buffer**: macOS PTY buffers are smaller and not tunable,
  reaching the I/O deadlock threshold sooner.

## Fix Plan

### 1. Throttle UI updates during streaming (high priority)

Add a debounce/coalesce mechanism so the TUI processes at most ~10–20
message update events per second, not one per token.

**Option A** — Throttle in `streamingWriter.Update()`:

```go
// Publish to pubsub at most every 50–100ms, not on every token.
func (w *streamingWriter) Update(ctx context.Context, msg message.Message) error {
    clone := msg.Clone()

    w.mu.Lock()
    defer w.mu.Unlock()

    w.pending = &msg
    now := time.Now()

    // Throttle both DB writes AND pubsub publishes.
    if now.Sub(w.lastPublish) >= streamingPublishInterval {
        w.svc.Publish(pubsub.UpdatedEvent, clone)
        w.lastPublish = now
    } else if w.publishTimer == nil {
        w.publishTimer = time.AfterFunc(streamingPublishInterval, func() {
            w.mu.Lock()
            defer w.mu.Unlock()
            if w.pending != nil {
                w.svc.Publish(pubsub.UpdatedEvent, w.pending.Clone())
                w.lastPublish = time.Now()
            }
            w.publishTimer = nil
        })
    }

    // ... existing DB write throttling ...
}
```

**Option B** — Coalesce in the TUI's `Update()` handler: when a message
update event arrives, start a short timer (~50ms). If more updates arrive
before it fires, reset the timer. Only apply the latest state when the
timer fires.

### 2. Incremental markdown rendering (medium priority)

Instead of re-rendering the full message on every update, append only the
new delta to the rendered output. This turns the O(n²) work into O(n).

Possible approach: track the last-rendered content length and only call
glamour on the new suffix, appending the result to the cached render.
Edge case: markdown constructs that span the boundary (e.g., a code fence
opened but not yet closed) need special handling.

### 3. Disable mouse tracking during streaming (low priority, quick win)

Set `v.MouseMode = tea.MouseModeNone` while the agent is actively
streaming. This eliminates the mouse event flood that contributes to the
PTY I/O deadlock.

```go
if m.isAgentRunning() {
    v.MouseMode = tea.MouseModeNone
} else {
    v.MouseMode = tea.MouseModeCellMotion
}
```

### 4. Ensure Ctrl+C always works (low priority)

Register a `signal.Notify` handler for `SIGINT` as a fallback outside
Bubble Tea's event loop, so that even if the event loop is starved, the
process can still be interrupted. Alternatively, use a dedicated goroutine
that reads raw stdin bytes looking for `0x03` (Ctrl+C) and forces shutdown.

## Files Involved

| File | Role |
|------|------|
| `internal/agent/streaming_writer.go` | Pubsub publish on every token (no throttle) |
| `internal/ui/chat/assistant.go:257` | `clearCache()` on every `SetMessage()` |
| `internal/ui/chat/assistant.go:213` | Full glamour re-render on cache miss |
| `internal/ui/model/ui.go:2533` | Mouse cell motion always enabled |
| `internal/ui/model/ui.go:722-743` | TUI update handler for message events |
| `internal/pubsub/broker.go` | 64-event buffer, drain-on-full |
| `internal/app/app.go:104` | 100-event buffer for all event types |

---

# TODO: Fix LSP Infinite Retry Loop

## Problem

When an LSP server (e.g. `gopls`) is not installed, the LSP manager
retries starting it on **every file operation** with no retry limit and no
backoff. This causes:

- Hundreds of error log entries per minute.
- Wasted CPU on repeated `exec.LookPath` / `New()` failures.
- Each retry triggers a pubsub callback that feeds into the TUI event
  loop, contributing to the event flood described above.

## Live Debug Evidence

Observed on a running smith instance (PID 502520, 100% CPU):

- `smith.log` contained **254 identical** "Failed to create LSP client"
  entries for `gopls` in ~12 minutes.
- The tmux window showed "Thinking..." with 100% CPU usage and was
  completely unresponsive.
- `strace` showed pure futex spinning — no actual I/O, just Go runtime
  lock contention from the hot retry loop.

## Root Cause

In `internal/lsp/manager.go:152-257`, the `startServer()` function has
an `exec.LookPath` guard that caches failures in the `unavailable` map
— but **only for auto-detected servers** (line 177: `if !isUserConfigured`).

For user-configured servers, when `New()` fails at line 212:

```go
client, err := New(ctx, name, cfg, ...)
if err != nil {
    slog.Error("Failed to create LSP client", "name", name, "error", err)
    return  // ← returns without marking as unavailable
}
```

The server is **never marked as `unavailable`**, so every subsequent call
to `Start()` retries from scratch. `Start()` is called from:

- `ui/model/ui.go:3491` — on every message send.
- `ui/model/session.go:453` — on file path discovery.
- `agent/tools/diagnostics.go:60,127` — on every diagnostics tool call.

## Fix Plan

### Option A: Mark failed servers as unavailable (simplest)

When `New()` fails for any reason (including user-configured servers),
mark the server in `unavailable` so it is not retried:

```go
client, err := New(ctx, name, cfg, ...)
if err != nil {
    slog.Error("Failed to create LSP client", "name", name, "error", err)
    unavailable.Set(name, struct{}{})
    return
}
```

### Option B: Retry with backoff and max attempts

Add a per-server failure counter with exponential backoff and a max retry
limit (e.g. 3 attempts). After exceeding the limit, mark as unavailable.

### Option C: Distinguish permanent vs transient failures

- "executable file not found" → permanent, mark unavailable immediately.
- Timeout / connection refused → transient, allow retry with backoff.

## Files Involved

| File | Role |
|------|------|
| `internal/lsp/manager.go:164-166` | `unavailable` cache check (passes because never set) |
| `internal/lsp/manager.go:177-187` | `exec.LookPath` guard — only for non-user-configured |
| `internal/lsp/manager.go:212-214` | `New()` failure — returns without caching |
| `internal/lsp/manager.go:242-246` | `Initialize()` failure — deletes client, enables retry |

---

# TODO: Fix tmux Window Accumulation

## Problem

When smith crashes or is killed inside tmux (e.g. via SIGABRT during
debug, or due to the rendering freeze), the tmux window is destroyed but
the tmux **session persists**. Re-launching smith attaches to the same
session and creates a **new window**. Over time, dead windows accumulate
(observed: 3 windows in one session, only 1 alive).

This is cosmetic but confusing. The `new-session -A` flag in the tmux
command (`internal/cmd/tmux.go`) attaches to an existing session rather
than creating a new one, but each attach creates a new window because the
original window's shell exited.
