# Agent Activity Tracking — Design

Date: 2026-09-08
Status: proposed

## Problem

AgentBoard spawns one coding agent per ticket, each in its own tmux window. With ten agents running, the user has to cycle through ten windows to find the one that is stuck on a permission prompt or asking a question. There is no signal on the board, no desktop notification, and no way to answer from anywhere but that pane.

Today the orchestrator knows only `AgentActive: bool` on a ticket and a single completion callback. Completion itself is inferred by polling `tmux list-panes` every 500 ms and watching `pane_current_command` change. Nothing detects "waiting for a human".

## Goal

Track every spawned agent's activity concurrently, surface "needs you" on the board and as a system notification, and provide one channel through which a remote client (TUI today, mobile app later) can approve a tool call or answer a question for a specific session.

Concurrency is a hard requirement: five Claude Code sessions in five tmux windows must all be tracked at the same time, whether or not their window is visible.

## Non-goals

- Replacing tmux as the display surface. The user still interacts in the pane.
- Headless / `-p` mode for Claude Code. Sessions stay interactive.
- Mobile push delivery. The design leaves a subscriber slot for it.
- Token or cost accounting from transcripts.

## Research summary

Three reference projects were studied along with the vendor docs.

| Project | Detection source | Answer channel |
|---|---|---|
| agent-orchestrator (Go, tmux) | Claude Code hooks installed per worktree, posting to a loopback daemon. Terminal scraping only as a stale-state reconciler. | `tmux send-keys -l` chunked, then `Enter`, gated by a guard that refuses writes into a blocked session. |
| monocode (Tauri) | Owns the child process, `--output-format stream-json --permission-prompt-tool stdio`. | `control_response` on stdin. |
| t3code (Node) | Owns the child through the Agent SDK `canUseTool` callback and Codex app-server JSON-RPC. | Resolve the pending callback. |

All three reject terminal regex as a primary source. The two that started with scraping or transcript parsing abandoned it. Because AgentBoard keeps agents interactive in tmux, the agent-orchestrator model is the fit: hooks are authoritative, screen capture is a fallback.

Vendor signal sources, ranked by reliability:

| Agent | Signal | Answer |
|---|---|---|
| Claude Code | Hooks passed per process with `--settings '<inline json>'`, `http` handler type. Events: `Notification` (permission_prompt, idle_prompt, agent_needs_input), `PermissionRequest`, `PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `Stop`, `SessionEnd`. Every payload carries `session_id`, `transcript_path`, `cwd`; tool events add `tool_name`, `tool_input`, `tool_use_id`. | `PermissionRequest` hook response `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"|"deny"}}}`. Free-text answers via `send-keys`. |
| OpenCode | Board-supplied plugin loaded per pane through `OPENCODE_CONFIG`. Its `chat.message`, `tool.execute.before`, `tool.execute.after` and `event` hooks POST Claude-Code-shaped payloads, so the board's Claude mapper and receiver are reused unchanged. | The plugin holds the `permission.asked` event open on the board's reply, then answers through `POST /session/{id}/permissions/{permissionID}` with `once` or `reject`. |
| Cursor CLI | `.cursor/hooks.json` in the worktree: `beforeShellExecution`, `beforeMCPExecution`, `stop`. | Hook stdout `{"permission":"allow"|"deny"}`. |
| Codex | `~/.codex/hooks.json`: `PermissionRequest`, `Stop`, `SessionEnd`. | Hook decision. |
| FreeCode (own agent, `~/Project/freecode`) | Built-in `board-webhook` hook inside FreeCode, enabled by `FREECODE_HOOK_URL`. Emits the same JSON shape as Claude Code hooks, so the board's Claude mapper and receiver are reused unchanged. | Same held `PermissionRequest` response; FreeCode maps `allow` to `allow-once` via `answerPermission` and `deny` to `rejectPermission`. |
| Unknown agent | `tmux capture-pane -p -e -J`, hashed, stable across several polls, plus a structural prompt match. | `send-keys -l`, pause, `Enter`, re-capture to verify. |

tmux's own `monitor-activity` / `monitor-silence` hooks fire only for windows that are not visible, so they cannot be the primary mechanism.

## Architecture

```
agent process ──hook POST──▶ /hooks/{sessionID} ──▶ ActivityService ──▶ store
                                                         │
                                                         ▼
                                                    EventHub (in-process)
                                                    ├── TUI (glyph, header count)
                                                    ├── Notifier (notify-send)
                                                    └── WebSocket (later, mobile)

TUI / remote ──answer──▶ ActivityService.Resolve ──▶ pending hook reply
                                                 └──▶ InputGuard ──▶ tmux send-keys
```

### Activity state

Per session, stored on the `sessions` row.

| State | Meaning |
|---|---|
| `active` | A turn is running. |
| `idle` | At an empty prompt, waiting for the next instruction. Safe to paste into. |
| `waiting_input` | The agent asked a question (AskUserQuestion). Safe to paste an answer. |
| `blocked` | A permission dialog is open. Any stray keystroke answers it. Automated writes must refuse. |
| `exited` | The agent process is gone. The pane holds a shell. |

`NeedsInput()` is `waiting_input || blocked`. `IsSticky()` is the same set: time-based reconciliation may never demote them.

Columns added to `sessions`: `activity_state`, `activity_at`, `launch_id`. New table `pending_requests(id, session_id, kind, tool_name, tool_input, created_at, resolved_at, decision)`.

### Launch generation

Every spawn generates a `launch_id` and passes it to the agent as `AGENTBOARD_LAUNCH_ID`. Hooks echo it. The receiver drops any signal whose launch id does not match the stored one. Without this, a resumed or relaunched agent's late hooks corrupt state. This is the single most important guard in agent-orchestrator.

### Hook receiver

A small `net/http` server bound to `127.0.0.1:0` at startup. The chosen port is passed to agents in `AGENTBOARD_HOOK_URL`. One route:

```
POST /hooks/{sessionID}
body: the agent's native hook JSON, unchanged
```

The handler maps the native payload to an `ActivitySignal{Event, State, ToolName, ToolUseID, LaunchID}` through a per-agent mapper, then calls `ActivityService.Apply`.

For `PermissionRequest` the handler holds the HTTP response open until the board resolves the request or a timeout elapses. Claude Code waits on the hook, so the reply body is the decision. On timeout the handler returns no decision, which lets the agent fall back to its own prompt in the pane. The user can still answer there.

### Verified against Claude Code 2.1.251

Read off the shipped binary's own hook schemas and confirmed by running `claude` against a logging receiver:

- `--settings` takes an inline JSON string. The http handler is `{"type":"http","url","headers","allowedEnvVars","timeout"}`; `timeout` is seconds and defaults to ten minutes.
- A header value interpolates `$AGENTBOARD_LAUNCH_ID` from the pane environment **only** when `allowedEnvVars` names it. Unlisted `$VAR` references resolve to the empty string.
- `PreToolUse`, `PostToolUse`, `PostToolUseFailure` and `PermissionDenied` carry `tool_use_id`. **`PermissionRequest` does not** — it carries `tool_name`, `tool_input`, `permission_suggestions`. The tool-flight rule therefore cannot key the blocked candidate on a `tool_use_id` taken from the dialog; it has only the tool name, which is exactly the ambiguous case the spec already fails closed on.
- `Notification` carries `message`, `title`, `notification_type`.
- `SessionEnd` reasons are `clear`, `resume`, `logout`, `prompt_input_exit`, `other`.
- The permission decision is nested: `decision: {behavior: "allow"|"deny"}`, not a bare string. FreeCode reads a bare `decision: "allow"`, so `Resolve` must write whichever shape the answering agent expects.
- Hooks run inline and block the turn, so lifecycle reports carry a 5 s timeout and only `PermissionRequest` is allowed to wait on a human.
- Observed in a `-p` run: `UserPromptSubmit`, `PreToolUse`, `PostToolUse` (same `tool_use_id`), `Stop`, `SessionEnd`. `SessionStart` did not fire in `-p` mode.

### State transitions (Claude Code mapper)

| Hook event | Resulting state |
|---|---|
| `SessionStart`, `UserPromptSubmit` | active |
| `PreToolUse`, `PostToolUse` | active, plus tool-flight bookkeeping |
| `PermissionRequest` | blocked, opens a pending request |
| `Notification: permission_prompt` | blocked |
| `Notification: agent_needs_input` | waiting_input |
| `Notification: idle_prompt` | idle |
| `Stop` | idle |
| `SessionEnd` | exited unless reason is `clear` or `resume` |

Tool-flight rule, taken from agent-orchestrator: while `blocked`, only a turn-boundary event (`UserPromptSubmit`, `Stop`, `SessionEnd`, process exit) or the `PostToolUse` of the exact tool that raised the dialog may change state. Parallel subagent tool traffic is otherwise suppressed. If two in-flight tools share a name, the candidate is ambiguous and the block clears only at a turn boundary. Fail closed.

### Notifier

Subscribes to the hub. Fires exactly once on entry into the needs-input family (`!prev.NeedsInput() && next.NeedsInput()`). Escalation from waiting_input to blocked does not re-notify. Leaving the family auto-resolves.

Policy: skip when the TUI is focused and the ticket is on screen. Delivery on Linux is `notify-send` with the session id in the notification body so a click can jump to the pane. The same subscriber shape carries a WebSocket or push sender later.

### Answering

`ActivityService.Resolve(sessionID, requestID, decision)`:

- If a hook is holding the request open, write the decision as the hook response.
- Otherwise the request came from a `Notification` and there is no hook to answer. Fall back to the InputGuard: re-read the session state, refuse if `blocked` (a keystroke could pick the wrong option), refuse if `exited` (the paste would run in a shell), else `send-keys -l` chunked at 16 KB, wait 300 ms, send `Enter`, re-capture and verify.

Every pane write goes through the InputGuard. The zero value of its result is "suppressed", so a forgotten assignment never reads as delivered.

### Fallback reconciler

A single goroutine polls every 30 s. For sessions in `active` with no hook signal for 2 minutes, it runs `capture-pane -p -e -J`, strips ANSI, and applies a structural check: last prompt marker line, whether a numbered option frame with a hint row sits above it, whether the composer is empty. It may write `idle`, `active`, or `waiting_input`. It never writes `blocked` or clears a sticky state. Sessions whose agent has full hook coverage and have sent no hook at all after 90 s show `no_signal` in the TUI instead of a confident idle.

### Spawn changes

`tmuxAgentRunner.Start` passes the following environment into the tmux window and, for Claude Code, appends `--settings` with inline JSON registering `http` hooks at `$AGENTBOARD_HOOK_URL/hooks/<sessionID>`:

```
AGENTBOARD_SESSION_ID
AGENTBOARD_LAUNCH_ID
AGENTBOARD_HOOK_URL
```

OpenCode keeps its PTY spawn. It has no settings flag, so the board exports
`OPENCODE_CONFIG=~/.agentboard/cache/opencode-config.json`, a document whose
only key is `plugin: ["file://~/.agentboard/cache/opencode-plugin.mjs"]`.
OpenCode merges plugin lists across config sources rather than replacing them,
so the user's own plugins survive. Neither file is per session: the plugin
reads `AGENTBOARD_SESSION_ID`, `AGENTBOARD_LAUNCH_ID` and
`AGENTBOARD_HOOK_URL` out of its own pane environment, and is inert without
them.

### OpenCode

The plugin is `internal/activity/opencode_plugin.mjs`, embedded in the binary
and written to the cache at spawn.

| Plugin hook | Posted as |
|---|---|
| plugin load | `SessionStart` |
| `chat.message` | `UserPromptSubmit` |
| `tool.execute.before` | `PreToolUse` with `tool_use_id` = `callID` |
| `tool.execute.after` | `PostToolUse` with the same id |
| `event: session.idle` | `Stop` |
| `event: question.asked` | `Notification`, `agent_needs_input`, with the event's `questions` array as `tool_input` |
| `event: permission.asked` (`permission.updated` on older builds) | `PermissionRequest`, awaited |

Lifecycle posts time out after 5 s and swallow their errors; the permission
post waits eleven minutes, just past the board's own ten-minute hold, so the
pane picker takes over rather than both sides giving up at once. The plugin
reads either decision shape — Claude Code's nested `{"behavior":...}` or
FreeCode's bare string — so the board needs no per-agent branch for it. A
decision of `allow` becomes `once`; `deny` becomes `reject`; no decision
leaves the pane picker alone. If the picker answered first, the reply 404s and
is ignored.

Questions are alert-only: OpenCode exposes no remote reply for them, so the
answer is typed in the pane and the board clears `waiting_input` at the next
turn boundary. The `question` tool is gated behind
`OPENCODE_ENABLE_QUESTION_TOOL` and is not offered in headless `opencode run`.

Not covered: OpenCode emits no session-end event, so process exit is still
detected by the pane monitor.

### FreeCode

FreeCode is a TypeScript agent split into a `freecode serve` JSON-RPC backend and a thin TUI. Its core already has a hook registry with 14 events, an in-process bus that publishes `permission.asked` and `question.asked`, a pending-promise permission flow with `answerPermission(requestId, answer)` and `rejectPermission(requestId)`, and a `question` tool. Nothing in the board needs a FreeCode-specific mapper.

Changes inside FreeCode:

- New `apps/core/src/hooks/builtin/board-webhook.ts`, registered from `initHooks` in `hooks/bootstrap.ts` next to the rtk hook. Inert unless `FREECODE_HOOK_URL` is set.
- It registers callback hooks for `SessionStart`, `TurnStart`, `PreToolUse`, `PostToolUse`, `Stop`, and POSTs a Claude-shaped body: `hook_event_name`, `session_id`, `tool_name`, `tool_use_id`, `tool_input`. `TurnStart` is sent as `UserPromptSubmit` because FreeCode's own `UserPromptSubmit` carries the system prompt. Every request carries `X-Agentboard-Launch: $AGENTBOARD_LAUNCH_ID`.
- It subscribes to `permission.asked` and POSTs `hook_event_name: PermissionRequest` with the tool name and args, waiting up to FreeCode's own 30 minute prompt timeout. A reply with `hookSpecificOutput.decision` of `allow` calls `answerPermission` with `allow-once`; `deny` calls `rejectPermission`; an empty reply leaves the pane picker to the user. The picker and the webhook race; whoever answers first wins, which FreeCode already handles.
- It subscribes to `question.asked` and POSTs `hook_event_name: Notification, notification_type: agent_needs_input`. The answer is typed in the pane; the board clears `waiting_input` at the next turn boundary.
- `executeTool` in `agent/loop.ts` adds `toolUseId: toolCall.id` to the hook context so PreToolUse and PostToolUse can carry the id the tool-flight rule needs.

Changes inside the board: for agent `freecode`, export `FREECODE_HOOK_URL=<hookBaseURL>/hooks/<sessionID>` alongside the three common variables. No `--settings` argument.

Not covered: FreeCode sends no `SessionEnd`; process exit is still detected by the pane monitor.

### TUI

- Ticket card glyph: `●` active, `○` idle, `?` waiting_input, `!` blocked, `×` exited, `~` no_signal.
- Header shows `N waiting` when any session needs input.
- A new `waiting` overlay lists needing sessions with tool name and truncated input; `y` / `n` resolves, `Enter` jumps to the pane.

## Data flow example

Five Claude Code sessions, windows 1 through 5. Session 3 hits `Bash(rm -rf build)`:

1. Claude Code fires `PermissionRequest` to `/hooks/<s3>` and waits.
2. Mapper produces `blocked`, opens pending request `r1` with tool name and input, records `tool_use_id` as the blocked candidate.
3. Hub publishes. TUI marks ticket 3 with `!`, header reads `1 waiting`. Notifier sends "AGT-03 needs approval: Bash rm -rf build".
4. User presses `y` in the waiting overlay. `Resolve(s3, r1, allow)` writes `{"decision":"allow"}` as the hook response.
5. `PostToolUse` for that `tool_use_id` arrives, block clears, state returns to `active`.

Sessions 1, 2, 4, 5 keep streaming their own hooks throughout. None of this depends on which window is visible.

## Error handling

- Hook receiver unreachable from the agent: the agent's own pane prompt still works. Failed hook deliveries are visible only in the pane, so the TUI's `no_signal` state is the tell.
- Store write fails in `Apply`: log and drop the signal. The next signal repairs state. Never crash the receiver.
- Hub subscriber slow: per-subscriber buffered channel, non-blocking publish, drop on overflow. A slow notifier must not stall a lifecycle write.
- Board restarts: tool-flight map is in memory. After restart a block clears only at a turn boundary. Fail-safe staleness, never a spurious clear.

## Testing

- `internal/activity`: table-driven tests for the state machine and tool-flight precedence, including the ambiguous same-name case and the launch-id mismatch drop.
- Hook receiver: `httptest` with real Claude Code payload fixtures for every event, plus the held-open `PermissionRequest` resolved from another goroutine and the timeout path.
- Notifier: entry-only firing, no re-fire on escalation, auto-resolve on exit.
- InputGuard: refuses on blocked and exited, sends on idle, with the fake tmux runner already used in orchestrator tests.
- Reconciler: fixture pane captures for active spinner, empty composer, numbered dialog. Must never write blocked.
- Integration (`-tags integration`): spawn a real `claude --settings` with a stub hook URL and assert the receiver sees `SessionStart`.

## Phases

1. Fix the four blocking orchestrator bugs first (see `2026-09-08-orchestrator-bug-report.md`): leaked monitor goroutines would corrupt the new state.
2. **Done for Claude Code.** Activity state, store migration, hub, hook receiver, Claude Code mapper, TUI glyph and header. Desktop notification.
3. Waiting overlay and `Resolve` with InputGuard.
4. Fallback reconciler.
5. OpenCode plugin plus board `OPENCODE_CONFIG` export. **Done.**
6. FreeCode built-in webhook plus board env export. **Done.**
7. Cursor and Codex mappers.
8. WebSocket subscriber for the mobile app.

## Open questions

- Whether to install hooks via `--settings` inline or write `.claude/settings.local.json` into the worktree. Chosen: neither exactly — the document is cached as `~/.agentboard/cache/settings-<sessionID>.json` and passed by path. It is still per process and still out of the worktree, but inline JSON turned the pane's launch line into a kilobyte of unreadable scrollback.
- Notification click-to-focus needs a helper that runs `tmux select-window`. `notify-send --action` is available on recent libnotify; otherwise the body just names the session.
