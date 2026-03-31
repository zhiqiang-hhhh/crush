# Session Mode Implementation Notes

This document captures the main design decisions, tradeoffs, and follow-up
considerations from the initial implementation of session-scoped modes in
Crush, starting with `plan` and later extended with `shell`.

## Goal

Add a planning workflow that feels like a real mode rather than a one-off
prompt trick:

- Users can switch modes with `/mode plan`, `/mode build`, and
  `/mode shell`.
- `/plan`, `/build`, and `/shell` act as shortcuts.
- Once selected, the mode stays active for the current session until it is
  changed again.
- `plan` should be read-only in practice, not just by instruction text.

## Final UX

- `build` is the default mode for new sessions.
- `plan` is session-scoped and persists across reloads.
- `shell` is session-scoped and persists across reloads.
- `/mode` reports the current mode.
- `/plan`, `/build`, and `/shell` switch mode without sending the text to
  the model.
- The command palette also exposes `Plan Mode (/plan)` and
  `Build Mode (/build)`, and `Shell Mode (/shell)`.
- Slash command discovery still works from an empty input via the existing
  command palette trigger.

## Why A Real Agent Instead Of A Prompt Wrapper

The first major decision was to avoid implementing plan mode as "send the
coder agent a planning prompt".

That approach is easy to add, but it has two problems:

1. It relies on prompt compliance instead of tool safety.
2. It does not model a real mode that can be routed consistently across a
   session.

The chosen design adds a dedicated `plan` agent in config. This keeps the
behavior explicit and makes routing decisions simple in the UI and
coordinator layers.

## Why Session-Scoped Persistence

The mode is stored on the session instead of only in UI memory.

Reasons:

- Switching sessions should restore the correct mode.
- Restarting the app should not silently drop mode state.
- A session is the natural unit for conversation behavior in Crush.

Implementation details:

- Added `mode` to the `sessions` table with default value `build`.
- Added `SessionMode` to `internal/session/session.go`.
- Added a focused `SetMode` session service method for lightweight updates.

## Why `/mode` Is The Primary Command

Several command shapes were possible:

- only `/plan` and `/build`
- a sticky `/plan` plus a separate exit command
- a generic `/mode <value>` command with aliases

The final design uses `/mode` as the canonical interface and keeps
`/plan`, `/build`, and `/shell` as convenience aliases.

This keeps the command surface small while leaving room for future modes
without inventing more one-off commands.

## Read-Only Boundaries For `plan`

The `plan` agent uses a dedicated prompt template, but the more important
guardrail is the tool set.

`plan` is limited to read-only tools using the existing
`resolveReadOnlyTools(...)` helper. That means the mode is structurally
safer than relying only on text instructions.

Current behavior:

- allowed: `glob`, `grep`, `ls`, `sourcegraph`, `view`
- excluded: `edit`, `write`, `multiedit`, `download`, `agent`, `todos`,
  and other mutating or side-effect-heavy tools

One consequence is that the current plan mode is intentionally conservative.
If we later want richer planning behavior, we can revisit whether read-only
`bash` should be available behind permission prompts.

## Coordinator Changes

Before this feature, the coordinator effectively assumed one main agent.

To support session mode routing without changing the rest of the app too
much, the implementation added `RunWithAgent(...)` while preserving the
existing `Run(...)` method as the default coder path.

This was the minimum change that made multi-agent routing practical.

Additional coordinator updates were needed so both primary agents behave
correctly:

- initialize both `coder` and `plan`
- refresh models, tools, and system prompts for all primary agents
- treat busy state, queue state, and cancellation as aggregate concerns

This keeps the rest of the UI simple because it does not need separate busy
handling logic per mode.

## UI Parsing Strategy

Mode commands are intercepted in the editor send path before normal message
submission.

Important behavior:

- only exact mode commands are intercepted
- valid commands update state and do not hit the model
- invalid commands show a warning
- normal text still goes through the active mode's routing path

This keeps the implementation local to the UI send path and avoids adding a
more global slash-command engine too early.

## Slash Command Palette Regression And Fix

One regression showed up quickly during implementation.

To allow users to type `/plan` into an empty input, the original `/` key
handling that opened the commands dialog was removed. That made slash
discovery worse because the slash prompt stopped appearing.

The fix was to keep both behaviors:

- empty input + `/` still opens the command palette
- typed `/mode`, `/plan`, `/build`, and `/shell` still work when submitted
- the command palette now also exposes explicit mode-switch entries

This preserves discoverability without giving up the new mode UX.

## Placeholder And Local UI Feedback

The current UI gives lightweight mode feedback in two ways:

- local info messages like `Mode changed to plan.`
- a `Plan mode` placeholder when the editor is idle in that mode

Shell mode extends that same pattern with a `Shell mode` placeholder.

## Why `shell` Is A UI Path Instead Of An Agent

Unlike `plan`, `shell` does not route through a dedicated LLM agent.

The goal of shell mode is direct command execution, not agent reasoning. The
implementation therefore handles it inside the UI send path:

- the user input is still stored as a normal user message
- the command is executed with `internal/shell`
- the output is written back as an assistant message
- no model request is made

This keeps shell mode aligned with the session/chat model without pretending
that a model produced the command output.

## Shell State And Tradeoffs

Shell mode uses a per-session in-memory `shell.Shell` instance so command
state can carry forward while the app is running.

That means commands like `cd tmp` or `export FOO=bar` affect later shell-mode
commands in the same session during that process lifetime.

Current tradeoffs:

- shell state is preserved while the app is running
- shell state is not restored after a full app restart
- attachments are ignored in shell mode
- command output is captured inline in chat instead of using a TTY handoff

This is a useful default because it preserves chat history and keeps the TUI
active, while still leaving room for a future interactive execution path.

This was intentionally kept small for the first pass. A more explicit status
indicator in the header or status bar would likely improve clarity further.

## Tests Added

Coverage was added in the areas most affected by the change:

- config tests for the new `plan` agent
- session tests for mode persistence
- coordinator tests for agent-specific routing
- UI tests for slash mode command parsing
- shell output formatting tests

The full test suite and build passed after the implementation.

## Known Limitations

The feature works, but a few follow-ups would make it more polished:

1. Show the current mode more prominently in the UI header or status area.
2. Add slash autocomplete that filters as the user types `/m`, `/pl`, etc.
3. Decide whether `plan` should eventually allow gated `bash` access.
4. Consider a TTY passthrough option for interactive shell commands.
5. Consider exposing mode in session list views or session metadata.
6. Revisit whether summaries should always use the coder agent or become
   mode-aware.

## Files Most Relevant To This Feature

- `internal/config/config.go`
- `internal/agent/prompts.go`
- `internal/agent/templates/plan.md.tpl`
- `internal/agent/coordinator.go`
- `internal/session/session.go`
- `internal/db/migrations/20260331000000_add_mode_to_sessions.sql`
- `internal/ui/model/mode.go`
- `internal/ui/model/ui.go`
- `internal/ui/dialog/actions.go`
- `internal/ui/dialog/commands.go`

## Summary

The key implementation idea is simple: mode is a session property, and the
session property determines which primary agent handles future prompts.

That keeps the UX intuitive, the data model durable, and the safety model
stronger than a prompt-only solution.
