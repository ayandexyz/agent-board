# AGENT.md — AgentBoard

## Project Overview

AgentBoard is a terminal-based Kanban board that orchestrates AI coding agents. It manages development tickets, spawns AI agents (Claude Code, OpenCode, Cursor) as tmux panes or custom in-Go PTY panes, decomposes projects into tickets via LLM APIs, and exposes a local HTTP/WebSocket API for a future Next.js frontend.

### Architecture Summary

```
┌─────────────────────────────────────────────────────┐
│                      TUI (bubbletea)                │
│  Kanban View │ Ticket Detail │ Agent Panes │ Help    │
├─────────────────────────────────────────────────────┤
│                   Orchestrator                       │
│  tmux Session Manager │ Agent Spawner │ PTY Capture  │
├─────────────────────────────────────────────────────┤
│  Decomposition │ MCP Client │ API Server │ Storage   │
│  (LLM tickets) │ (ContextCarry, │ (chi + WS) │ (SQLite)│
│                │  SessionCarry) │             │         │
├─────────────────────────────────────────────────────────┤
│  API Types     │ MCP Client  │                            │
│  (DTOs)        │ (reusable)  │                            │
├─────────────────────────────────────────────────────┤
│                    Config                            │
│  env vars │ ~/.agentboard/config.toml │ agent detect │
└─────────────────────────────────────────────────────┘
```

Data flows down: TUI renders state from the orchestrator, which coordinates agents, storage, and MCP connections. The API server mirrors TUI actions for remote control.

---

## Folder Structure

```
agent-board/
├── cmd/agentboard/         # Entrypoint
│   └── main.go             # Wire dependencies, start TUI or API mode
├── internal/
│   ├── tui/                # Bubble Tea TUI layer
│   │   ├── app.go          # Root bubbletea.Model, window management
│   │   ├── kanban.go       # Kanban board rendering and columns
│   │   ├── keybindings.go  # Key mapping and handler dispatch
│   │   ├── ticketview.go   # Ticket detail/edit panel
│   │   └── pane.go         # Embedded agent pane (PTY-in-a-widget)
│   ├── orchestrator/       # Session and agent lifecycle
│   │   ├── service.go      # Orchestrator service, Runners (TmuxRunner, PtyRunner)
│   │   ├── pane_manager.go # tmux window/pane management for agents
│   │   ├── pty_runner.go   # PTY-based agent runner
│   │   ├── tmux_runner.go  # tmux-window-based agent runner
│   │   ├── types.go        # Interfaces and types (Runner, Store, LLMClient)
│   │   ├── actions.go      # Run outcome processing
│   │   ├── approval.go     # Proposal approval logic
│   │   └── summarizer.go   # Context summarization
│   ├── mcp/                # MCP server integrations
│   │   ├── client.go       # Shared MCP client bootstrap and registry
│   │   ├── contextcarry.go # ContextCarry MCP server integration
│   │   └── sessioncarry.go # SessionCarry MCP server integration
│   ├── api/                # Loopback HTTP API over the orchestrator
│   │   ├── server.go       # Server, Board/Activity interfaces, loopback guard
│   │   ├── handlers.go     # REST handlers under /api/v1
│   │   ├── events.go       # Event hub and SSE /events stream
│   │   └── terminal.go     # WebSocket terminal over a tmux viewer session
│   ├── store/              # SQLite persistence
│   │   ├── sqlite.go       # DB connection, initialization, migrations
│   │   ├── tickets.go      # Ticket CRUD operations
│   │   ├── sessions.go     # Session and agent state persistence
│   │   └── migrations.go   # Schema migration definitions
│   ├── decomposition/      # LLM-powered project breakdown
│   │   ├── decomposer.go   # Project → tickets decomposition engine
│   │   ├── assigner.go     # Auto-assign tickets to agents with reasoning
│   │   └── prompts.go      # Prompt templates for decomposition and assignment
│   ├── agents/             # Catalog of coding agents — one file per agent
│   │   ├── agents.go       # Capability, Descriptor, Register/Lookup registry
│   │   ├── catalog.go      # The list of agents that exist, in picker order
│   │   └── claude.go …     # One descriptor per agent (opencode, freecode, …)
│   ├── apitypes/           # Shared API types (DTOs)
│   │   ├── ticket.go       # Ticket DTOs
│   │   ├── session.go      # Session DTOs
│   │   └── agent.go        # Agent DTOs and status enums
│   ├── mcpclient/          # Reusable MCP client wrapper
│   │   └── client.go       # Generic MCP client for future external consumers
│   └── config/             # Configuration and environment
│       ├── config.go       # Config struct, TOML loading, env var overlay
│       ├── detection.go    # Auto-detect available agents on $PATH
│       └── defaults.go     # Default values and config scaffolding
├── go.mod
├── go.sum
└── AGENT.md                # This file — project memory
```

---

## Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| **internal/ for all private packages** | Enforces encapsulation; no external consumer can depend on internals. |
| **Everything in internal/ until needed externally** | `apitypes` and `mcpclient` stay in `internal/` — will graduate to `pkg/` when the Next.js frontend becomes a real external consumer. No premature abstraction. |
| **Interfaces defined where used** | `orchestrator` defines `AgentSpawner`; `internal/tui` defines `Renderer`. Implementations live in their own package. Prevents circular imports. |
| **modernc.org/sqlite over CGO sqlite** | Pure Go, compiles on Termux (Android ARM64) without a C toolchain. Opened in WAL mode with a 5 s busy timeout (`store.dsn`) so a CLI command and the TUI can share the file. |
| **stdlib `http.ServeMux` over chi** | Go 1.22 method+path patterns cover every route; no extra dependency for Termux builds. |
| **Container / presentation** | `orchestrator.Service` is the one container: every rule (first-column create, branch-mode switch, status moves, merges) lives there. `internal/tui` and `internal/api` are two presentations of it and hold no `*store.Store`. A rule that only one front end knows is a bug. |
| **API binds loopback only** | `api.Server.Start` refuses a non-loopback address. A phone reaches it through `tailscale serve` (or an SSH forward), so the API needs no auth of its own. |
| **bubbletea + custom pane over pure tmux** | Toggle between tmux-managed panes (for existing tmux users) and embedded PTY panes (for standalone usage). User chooses at startup. |
| **TOML config + env vars** | TOML for persistent user preferences; env vars for CI/overrides. Env vars take precedence. |
| **Error wrapping with `fmt.Errorf("context: %w", err)`** | Consistent error chain for debugging. Every package wraps errors at boundary points. |
| **TDD — tests first, always** | Every feature starts with a failing test. No implementation code without a corresponding `_test.go`. This is non-negotiable. |
| **AGENT.md as project memory** | Single source of truth for architecture, conventions, and onboarding. Updated as the project evolves. |
| **MCP via mark3labs/mcp-go** | Type-safe Go MCP client. Used to connect to ContextCarry and SessionCarry npm servers via stdio transport. |

---

## Build and Run

```bash
# Build
go build -o agentboard ./cmd/agentboard

# Run (TUI mode, default)
./agentboard

# The HTTP API starts with the TUI on 127.0.0.1:7878 ([general] addr / api)
curl localhost:7878/api/v1/board
curl -N localhost:7878/api/v1/events
# From another machine, through SSH:
ssh -L 7878:127.0.0.1:7878 host   # then the same curls locally

# Initialize config
./agentboard init

# Run tests
go test ./...

# Run with verbose logging
AGENTBOARD_LOG=debug ./agentboard
```

### Dependencies

```bash
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/lipgloss
go get github.com/charmbracelet/bubbles
go get github.com/go-chi/chi/v5
go get github.com/mark3labs/mcp-go
go get modernc.org/sqlite
go get github.com/BurntSushi/toml
```

---

## Core Data Model

### Ticket

```
id          TEXT PRIMARY KEY  (e.g. AGT-03)
title       TEXT
prompt      TEXT
status      TEXT  (backlog | in_progress | review | done)
agent       TEXT  (claude-code | opencode | cursor | null)
branch      TEXT
created_at  DATETIME
updated_at  DATETIME
depends_on  TEXT  (comma-separated ticket ids)
```

### Session

```
id             TEXT PRIMARY KEY
ticket_id      TEXT FK → tickets.id
agent          TEXT
started_at     DATETIME
ended_at       DATETIME
status         TEXT  (running | completed | failed | cancelled)
context_key    TEXT  (ContextCarry reference)
activity_state TEXT  (active | idle | waiting_input | blocked | exited, '' if never reported)
activity_at    DATETIME
launch_id      TEXT  (per-spawn id the agent's hooks must carry)
```

### PendingRequest

```
id          TEXT PRIMARY KEY  (e.g. REQ-03)
session_id  TEXT FK → sessions.id
ticket_id   TEXT
kind        TEXT  (permission)
tool_name   TEXT
tool_input  TEXT  (raw JSON)
created_at  DATETIME
resolved_at DATETIME
decision    TEXT  (allow | deny | pane | timeout | dropped | abandoned)
```

---

## tmux Session Layout

AgentBoard creates a tmux session named `{project-name}` when run outside of tmux ([main.go:28-40](cmd/agentboard/main.go#L28-L40)).

```
┌──────────────────────────────────────────────────────────┐
│  tmux session: {project-name}                            │
│                                                          │
│  Window 0: agentboard (TUI - bubbletea kanban)           │
│  Window 1: agent-{sessionID} (opencode/claude agent)     │
│  Window 2: agent-{sessionID} (another agent)             │
│  ...                                                     │
└──────────────────────────────────────────────────────────┘
```

- **Window 0**: AgentBoard TUI (bubbletea kanban view)
- **Window N (N>0)**: One per active agent, named `agent-{shortSessionID}`
  - Created by `PaneManager.CreatePane()` or `PtyRunner.Start()`
  - User switches to these windows to interact with agents directly
- Dashboard view ([app.go:474-502](internal/tui/app.go#L474-L502)) creates a split pane to show agent output via `tmux attach-session`
- Agent windows are destroyed when their ticket moves to Done or is cancelled

---

## Agent Spawning

When not already in tmux, agentboard creates a new tmux session named `{project-name}` and re-executes inside it ([main.go:28-40](cmd/agentboard/main.go#L28-L40)).

When already in tmux, two runners are initialized ([main.go:58-68](cmd/agentboard/main.go#L58-L68)):
- `TmuxRunner` ← backed by `PaneManager` → creates tmux **windows** for agents via `tmux new-window`
- `PtyRunner` ← backed by `pty.PtyRunner` → allocates real PTY and runs agent process; also creates tmux window for display

**Active path**: `service.go:254-273` uses `ptyRunner.Start()` if available, otherwise `TmuxRunner.Start()`.

### TmuxRunner / PaneManager flow

1. `PaneManager.CreatePane()` creates a tmux window named `agents-{ticketID}`
2. Writes prompt to `~/.agentboard/cache/prompt-{sessionID}.txt`
3. Sends `agent run "$(cat promptFile)"` via `tmux send-keys`
4. Monitors pane for completion by polling `tmux list-panes`

### PtyRunner flow

1. Creates a tmux window named `agent-{shortSessionID}` in the session
2. Starts agent binary in a real PTY via `pty.Start()`
3. Waits for ready pattern (e.g., "Ask anything" for opencode)
4. Injects prompt character-by-character via PTY write (with 10ms delays)
5. Monitors for done marker or idle patterns to detect completion

### Dashboard integration

When viewing dashboard with active sessions ([app.go:474-502](internal/tui/app.go#L474-L502)):
- Creates a vertical split pane via `tmux split-window -h -p 70`
- Runs `tmux attach-session -t agentboard-{ticketID}` in the split to show agent output
- User can interact with the agent directly in that tmux window

### Agent CLI Commands

| Agent | Spawn Command |
|-------|---------------|
| Claude Code | `claude --no-autocomplete` then prompt via PTY |
| OpenCode | `opencode` then prompt via PTY character-by-character |
| Cursor | Connected via MCP (no direct CLI spawn) |

**Note**: OpenCode is interactive-first — it has no prompt flag. The PtyRunner waits for the ready pattern ("Ask anything"), then injects context via PTY write with per-character delays to simulate typing.

---

## Agent Activity Tracking

Implemented for Claude Code, FreeCode and OpenCode.

Spawned agents report their own state over HTTP rather than the board guessing from the screen.

`internal/activity` is the receiver: a loopback `net/http` server on `127.0.0.1:0`, started in `runTUI` and handed to the orchestrator via `SetHookBaseURL`. It accepts `POST /hooks/{sessionID}` carrying Claude-Code-shaped hook JSON, maps it to a state (`active`, `idle`, `waiting_input`, `blocked`, `exited`), and fires one `notify-send` on entry into the needs-input family. The alert body is `toolDetail` in `internal/activity/detail.go`: the question for `AskUserQuestion`, the command for `Bash`, the path for a file edit, and the bare tool name for anything unlisted — never a dump of `tool_input`. A permission request naming `AskUserQuestion` is `waiting_input`, not `blocked`; what is waiting is an answer, not an approval. It is also never held open or marked `Decidable` — allowing it would only let the agent ask, and the answer is typed in the pane regardless — so Claude Code's questions raise the same in-app notification OpenCode's and FreeCode's do, rather than an approve/deny modal that would answer nothing. `X-Agentboard-Launch` is checked against the launch id recorded at spawn, so a relaunched session's late hooks are dropped.

`PermissionRequest` is held open (10 minutes by default, `Receiver.PermissionTimeout`) while the board asks: the alert is flagged `Decidable`, the TUI raises an approval modal instead of a toast, and `Receiver.Resolve(sessionID, decision)` writes the reply — nested `{"behavior":...}` for Claude Code and OpenCode, a bare string for FreeCode, through the `activity.DecisionCodec` recorded at `Launch`. Cancelling the modal denies; a timeout answers `{}` and the pane picker takes over. The receiver never names an agent: a nil codec simply means nobody can be answered over the wire.

### The agent catalog

`internal/agents` is the one place that knows one agent from another. Each agent is a `Descriptor` in its own file — name, binary, whether it is offered in the picker, its logo and colour, its resume pattern, the capabilities the board may exercise, its `activity.DecisionCodec`, and a `Provision` func that wires one spawn up to the receiver. `catalog.go` lists them in the order they are offered; `Register` panics at startup on a contradictory descriptor (claiming remote permissions with no codec, provisioning reporting it never claimed) rather than failing silently at the moment a user is waiting on a modal.

Adding an agent is adding one file and one line in `catalog.go`. `config.DetectAgents`, `orchestrator.ExtractResumeCommand` and `Service.launchSpec` all read the catalog, so none of them grows a case.

Capabilities are added only when something branches on them. Today:

| Capability | What branches on it |
|---|---|
| `activity.reports` | `launchSpec` provisions and notifies the receiver only for agents that report |
| `permission.remote` | must agree with the presence of a decision codec; enforced at `Register` |

Deliberately not yet declared, because nothing reads them: `lifecycle.session_end` (OpenCode reports no exit, so only the pane monitor notices) and `tool.flight_ids` (no `tool_use_id` means any `PostToolUse` clears a permission block, not just the one that raised it).

### The alert hub

`Receiver.Subscribe()` returns a stream of alerts and a cancel func. Every subscriber gets its own `eventBuffer`-deep channel and sees every alert raised after it subscribed, so the TUI, an API websocket and a push bridge never take alerts from one another — which a single shared channel did. Sends are non-blocking and drop on overflow: a missed toast costs a notification, a blocked hook stalls the agent. Cancel closes the stream (idempotent), and `Receiver.Close` ends every subscription so readers unblock.

### Persistence

The receiver writes through to the store when `Receiver.Store` is set (`run.go` sets it to the SQLite store): `Launch` records `sessions.launch_id`, every accepted signal records `sessions.activity_state` and `activity_at`, and a held `PermissionRequest` becomes a row in `pending_requests` (`REQ-NN`, tool name and raw `tool_input`) that is closed with the decision it got — `allow`, `deny`, `pane` when the board handed the choice back, `timeout` when the hold expired, `dropped` when the agent hung up first. Writes are best effort: a hook never stalls on the database and in-memory state stays authoritative for the running process.

`Receiver.Restore`, called before `Start`, loads the last known state and launch id of every session that has not ended, so a restarted board still shows which agents were blocked, and closes any request left open as `abandoned` — the hook that held it died with the previous process. Note that the receiver binds a fresh port on every start, so agents launched by a previous board process cannot reach the new one; their restored state is the last thing the old process heard.

### Typing into a pane

Every keystroke the board sends to an agent goes through `Service.SendInput`, which asks the receiver's `StateOf` immediately before writing (`SetActivityState`, wired in `run.go`) and refuses with `ErrInputSuppressed` while the session is `blocked` — Enter would answer the permission dialog — or `exited`, where the pane holds a bare shell. The dashboard shows the refusal as a notification. Text is typed with `sendKeysChunked`: `send-keys -l` in 16 KiB chunks, a 300 ms pause, then `Enter`; the launch prompt uses the same path with newlines replaced by spaces, since Enter submits in every agent's composer.

### On the board

The TUI reads agent state through `tui.ActivityReader` (`StateOf`, `Waiting`), which the receiver satisfies. `App.refreshActivity` maps live sessions to their tickets on every animation tick, alert, run start and run completion; each card's footer then carries `ActivityGlyph` (`●` active, `○` idle, `?` waiting for an answer, `!` blocked on a permission, `×` exited) and the header shows `N waiting` when any session needs a human. `W` opens the waiting overlay: one row per waiting session, `y`/`n` resolve a held permission through the same `ResolvePermission` the approval modal uses (only offered when a hook is actually holding the request), `Enter` switches to the agent's tmux window. A question is never decidable from the overlay — the answer is typed in the pane.

Not built yet: the tool-flight rule beyond "any PostToolUse clears the block", the notification policy (suppress when the ticket is on screen), and the capture-pane reconciler.

Every agent pane is launched with its session identity exported via `tmux new-window -e`:

| Variable | Meaning |
|---|---|
| `AGENTBOARD_SESSION_ID` | The session this pane belongs to |
| `AGENTBOARD_LAUNCH_ID` | Per-spawn id; lets the board drop hooks from an earlier launch of the same session |
| `AGENTBOARD_HOOK_URL` | Base URL of the board's local hook receiver |
| `FREECODE_HOOK_URL` | FreeCode only: `<base>/hooks/<sessionID>` |
| `OPENCODE_CONFIG` | OpenCode only: path to the config document that loads the board's reporting plugin |

Claude Code takes no environment variable of its own. It is launched as
`claude --settings ~/.agentboard/cache/settings-<sessionID>.json`, whose
contents are built by `activity.ClaudeSettingsJSON`,
registering an `http` hook at `<base>/hooks/<sessionID>` for `SessionStart`,
`UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PermissionRequest`,
`Notification`, `Stop` and `SessionEnd`. The launch id rides along as the
`X-Agentboard-Launch` header, interpolated from `$AGENTBOARD_LAUNCH_ID` in the
pane — which Claude Code only does because `allowedEnvVars` lists it. The
document is cached next to the prompt file rather than passed inline: a
kilobyte of JSON typed into the pane buries the launch line in the user's
scrollback. It stays out of the ticket worktree either way.

Hooks block the turn, so lifecycle reports time out after 5 s; only
`PermissionRequest` waits (600 s), since that one is a human decision. Note the
two agents disagree on the reply shape: Claude Code wants
`decision: {"behavior": "allow"}` nested, FreeCode wants a bare
`decision: "allow"`. `PermissionRequest` from Claude Code carries no
`tool_use_id`.

The document reaches the pane through `RunRequest.Settings`, built by the
agent's `Provision` in `internal/agents/claude.go`; `launchArgs` in
`internal/orchestrator/pty_runner.go` caches it and `agentCommand` quotes the
path into the launch line.

Nothing is exported unless `Service.SetHookBaseURL` was called, so an agent is never told to reach a receiver that is not running.

OpenCode has no settings flag of its own, so the board ships the reporter as an
OpenCode plugin: `internal/activity/opencode_plugin.mjs`, embedded in the binary
and written by `activity.OpenCodeConfig` to
`~/.agentboard/cache/opencode-plugin.mjs` alongside a one-key config document
(`plugin: ["file://<that path>"]`) named by `OPENCODE_CONFIG`. OpenCode merges
plugin lists across config sources, so the user's own plugins survive. Neither
file is per session — the plugin reads `AGENTBOARD_SESSION_ID`,
`AGENTBOARD_LAUNCH_ID` and `AGENTBOARD_HOOK_URL` from its pane and stays inert
without them.

The plugin posts Claude-Code-shaped payloads, so there is no OpenCode mapper in
Go: `chat.message` as `UserPromptSubmit`, `tool.execute.before`/`after` as
`PreToolUse`/`PostToolUse` (`tool_use_id` = OpenCode's `callID`), `session.idle`
as `Stop`, `question.asked` as an `agent_needs_input` notification carrying the
event's own `questions` array (already the shape `toolDetail` reads for
`AskUserQuestion`, so the alert quotes the question), and `permission.asked` as
a `PermissionRequest` it awaits. `allow`
replies become `POST /session/{id}/permissions/{permissionID}` with `once`,
`deny` becomes `reject`, and no decision leaves the pane picker to the user. It
reads either decision shape, nested or bare. Questions are answered in the pane
— OpenCode exposes no remote reply for them — and OpenCode emits no session-end
event, so process exit is still detected by the pane monitor.

`internal/activity/opencode_plugin_probe.mjs` drives the plugin without
OpenCode, so a test can feed it one event and watch what reaches the receiver.
Note the `question` tool is gated behind `OPENCODE_ENABLE_QUESTION_TOOL` and is
not offered in headless `opencode run`.

FreeCode support lives in the FreeCode repo at `apps/core/src/hooks/builtin/board-webhook.ts`, registered from `initHooks`. It posts Claude Code shaped hook payloads (`SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop`) so the board needs no FreeCode-specific mapper, and it answers permission requests remotely: a reply of `{"hookSpecificOutput":{"decision":"allow"}}` resolves the agent's pending prompt as a one-shot grant, `deny` rejects it, and no decision leaves the choice to the pane picker.

See `docs/superpowers/specs/2026-09-08-agent-activity-tracking-design.md` and `docs/superpowers/plans/2026-09-08-agent-activity-tracking.md`.

---

## HTTP API

`internal/api` is the second presentation of the orchestrator, started in
`runTUI` next to the activity receiver when `[general] api = true`
(default). It binds `[general] addr`, default `127.0.0.1:7878`, and
`Start` refuses anything that is not loopback: a remote client (the
planned iOS app, a script) reaches it through `ssh -L` or a tailnet, so
the API carries no token of its own. It runs inside the TUI process
because active sessions live in the orchestrator's memory, not SQLite.

`api.Board` is the interface the server needs from `orchestrator.Service`
and `api.Activity` what it needs from `activity.Receiver`; both are
consumer-side interfaces with test fakes in `api_test.go`. DTOs live in
`internal/apitypes` with no board imports so a client can vendor them.

Routes, all under `/api/v1`:

| Route | Does |
|---|---|
| `GET /identity` | name, version, project, tmux session |
| `GET /board` | tickets grouped by configured status, each with its live `activity` and `session_id`; `waiting` count |
| `GET /tickets?status=&search=` · `POST /tickets` | list / create (lands in the first column) |
| `GET` · `PATCH` · `DELETE /tickets/{id}` | read / partial edit (status and branch mode excluded) / delete |
| `POST /tickets/{id}/status` `{status}` | `MoveTicketStatus` |
| `POST /tickets/{id}/branch-mode` `{mode}` | `SetBranchMode`: `current` clears the branch, `worktree` names the default |
| `POST /tickets/{id}/start` `{agent?, prompt?}` | `StartAdHocRun` on the ticket's own agent and prompt unless overridden |
| `POST /tickets/{id}/complete` | `CompleteTicket`; `409` with `merge_required` when unmerged work exists |
| `POST /tickets/{id}/merge` `{strategy?}` | `MergeTicket` |
| `GET /sessions` | active sessions with tmux window, activity state, held tool and `decidable` |
| `GET /sessions/{id}/screen?lines=` | `capture-pane` text |
| `POST /sessions/{id}/stop` · `/input` `{text}` · `/resolve` `{decision}` | stop, type into the pane (refused `409` while blocked/exited), answer a held permission |
| `GET /events?after=<seq>` | SSE stream |
| `GET /terminal?cols=&rows=` · `GET /sessions/{id}/terminal` | WebSocket: PTY bytes out as binary frames, keystrokes in as binary, `{"type":"resize","cols","rows"}` as text |

The terminal routes attach a **viewer**: `tmux.Attach` creates a grouped
session (`<session>-view-<rand>`) on the board's tmux session, selects the
agent's window in it, and attaches inside a PTY. Grouped, not shared, so a
phone switching windows or resizing never moves or shrinks the desktop
TUI. Closing the socket kills the viewer session only.

Every request carrying an `Origin` header is refused with 403: a page in
the user's desktop browser could otherwise POST to the loopback API.
Native apps, curl and SSH forwards send no Origin. A browser front end
will need an allowlist there, not the removal of the check.

To reach the API from a phone, publish the loopback port to the tailnet:
`tailscale serve --bg 7878`. Tailscale terminates HTTPS and the tailnet
ACL is the auth; the server still binds loopback only.

Errors map sentinels to status codes: `store.ErrNotFound` 404;
`ErrInvalidStatus`, `ErrInvalidPriority`, `ErrTitleRequired`,
`ErrInvalidBranchMode`, `ErrUnknownAgent` 400; `ErrTicketBusy`,
`ErrInputSuppressed` 409.

The event stream is an in-memory ring of the last 256 events with a
process-wide sequence. `run.completed` comes from
`Service.SubscribeCompletions`, `activity.alert` from
`Receiver.Subscribe`, and the API publishes `ticket.created`,
`ticket.changed`, `ticket.deleted`, `ticket.merged`, `session.started`,
`session.stopped`, `permission.resolved` for its own mutations. Edits made
in the TUI are not on the stream yet; a client refreshes `/board` on any
event or on an interval. A reconnecting client sends `?after=` or
`Last-Event-ID`; older than the ring it must refetch the board.

Not built yet: a headless `agentboard serve` for machines with no TUI,
TUI-side mutations on the stream, and push notifications for a phone in
the background. The mobile client is the Expo app in
`~/Projects/AIChat`; see
`docs/superpowers/specs/2026-09-12-mobile-client-design.md`.

---

## Git Worktree Isolation & Merge Flow

Every ticket gets an isolated git worktree when an agent starts on it, so parallel agents never collide on the filesystem.

### Branch mode (per ticket)

The ticket's Branch row is a toggle, not a text field: `e` on it opens
**Current branch | Worktree branch**.

- `worktree` (default, `tickets.branch_mode`): existing behaviour — the agent
  gets its own worktree and `agent/<ticket-id>` branch, and `m` (merge) offers
  to merge that branch before the ticket is done.
- `current`: no worktree. `prepareWorkDir` returns an empty work dir so the
  agent runs in the board's own checkout on whatever branch is already out, and
  `CompleteTicket` has nothing to merge.

Choosing "Worktree branch" drops straight into an edit box prefilled with the
default branch name so it can be renamed before the agent starts. Choosing
"Current branch" clears `tickets.branch`. `store.Ticket.UsesWorktree()` is the
single predicate everything else reads.

### Lifecycle

```
Backlog -> In Progress            -> Review              -> Done
           worktree + branch         `m` merges            worktree removed,
           created, agent cwd        (modal in TUI)        branch deleted
```

- **Spawn** (`orchestrator.Service.prepareWorkDir`): `git worktree add ~/.agentboard/worktrees/<project>/<ticket-id> -b agent/<ticket-id> <base>` (branch from `ticket.Branch` if set), tmux window opens with `-c <worktree>`. Path persisted in `tickets.worktree_path`; `worktree.created` event recorded.
- **Review -> Done is not gated**: `Service.MoveTicketStatus` is a plain status change for every transition, so a ticket can be moved to done with its worktree and branch left in place. Merging is explicit: `m` on a review ticket calls `Service.CompleteTicket`, which returns a typed `*MergeRequiredError` when unmerged work exists (the TUI turns that into the merge confirm modal; press `s` in the modal to switch strategy for this merge), cleans up and moves to done when the ticket's PR already reports MERGED, and otherwise moves the ticket to done. `Service.MergeTicket(ctx, id, strategy)` performs the merge and moves the ticket to done itself.
- **Strategies** (`[git] merge_strategy`, default `local`):
  - `local`: rebase the ticket branch onto the base branch *inside the worktree*, then `git merge --ff-only` in the repo root (root must be clean and on the base branch). Conflicts abort the rebase, emit a `merge.conflict` event, and leave the ticket in review.
  - `pr`: push the branch and `gh pr create`; PR URL stored in `tickets.pr_url`. Pressing `m` again once the PR reports MERGED (`gh pr view --json state`) runs cleanup with a forced branch delete (squash-safe) and moves the ticket to done.
- **Guardrails**: merges, PR-finalize and delete refuse while an agent session is active or still being spawned, and a run refuses to start (`ErrTicketBusy`) while a merge or delete holds the ticket — `Service.reserve` takes the claim under the same lock as the active-session check, so neither can slip in between the other's check and act. Cleanup runs before a merge moves the ticket to done: if the worktree cannot be removed the ticket stays in review with its path intact and merging again retries. Dirty worktrees are never auto-removed (ticket delete also refuses); no git repo -> all of this degrades to the old behavior.

### Key packages

- `internal/git` — pure git/gh command wrappers over an injectable `Runner` closure (`ExecRunner()` in production, fakes in tests; no exec in unit tests).
- `internal/orchestrator/worktrees.go` — `WorktreeManager` (implements `Worktrees` + `Merger`).
- `internal/orchestrator/merge.go` — `MergeTicket`, `CompleteTicket`, `MoveTicketStatus`, `DeleteTicket`, `MergeRequiredError`.
- Integration tests against real git: `go test -tags integration ./internal/orchestrator/`.

### Config (`[git]` in config.toml)

| Key | Default | Description |
|-----|---------|-------------|
| `merge_strategy` | `local` | `local` (rebase + ff-only) or `pr` (push + gh pr create) |
| `worktree_root` | `~/.agentboard/worktrees/<project>` | Where ticket worktrees live |
| `branch_prefix` | `agent/` | Branch name prefix when ticket has no branch |
| `base_branch` | auto-detect `main`/`master` | Merge target |

---

## MCP Integration

### ContextCarry

- **Package**: `contextcarry` npm package
- **Purpose**: Persists AI session context across conversations. AgentBoard uses it to restore agent memory when reassigning tickets.
- **Integration**: `internal/mcp/contextcarry.go` starts ContextCarry as a stdio subprocess, connects via mcp-go client.
- **Usage**: Before spawning an agent on a ticket, load prior context from ContextCarry. After agent finishes, save context.

### SessionCarry

- **Package**: `sessioncarry` npm package
- **Purpose**: Manages session state across multiple AI agent instances.
- **Integration**: `internal/mcp/sessioncarry.go` mirrors ContextCarry pattern.
- **Usage**: Track which agent worked on which ticket, store cross-session learnings.

### MCP Client Lifecycle

```
1. Detect npm + node on $PATH
2. npx contextcarry serve (stdio transport)
3. Connect mcp-go client
4. Call tools: save_context, load_context, list_sessions
5. Graceful shutdown on exit
```

---

## Keybinding Reference

| Key | Action |
|-----|--------|
| `h/l` or `←/→` | Move between Kanban columns |
| `j/k` or `↑/↓` | Move between tickets in column |
| `Enter` | Open ticket detail view |
| `a` | Add new ticket |
| `d` | Delete ticket (with confirmation) |
| `s` | Start agent on selected ticket |
| `m` | Merge ticket (Review only): opens the merge modal when unmerged work exists, otherwise moves to Done |
| `W` | Waiting agents overlay: `y`/`n` answer a held permission, `Enter` jumps to the pane |
| `x` | Stop running agent |
| `r` | Refresh board state |
| `Tab` | Toggle focus between board and agent pane |
| `1-4` | Jump to column (Backlog, In Progress, Review, Done) |
| `?` | Show help overlay |
| `q` | Quit |
| `Ctrl+C` | Force quit |

---

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `AGENTBOARD_CONFIG` | `~/.agentboard/config.toml` | Path to config file |
| `AGENTBOARD_DB` | `~/.agentboard/board.db` | SQLite database path |
| `AGENTBOARD_LOG` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `AGENTBOARD_ADDR` | `127.0.0.1:7878` | HTTP API bind address; must be loopback |
| `AGENTBOARD_API` | `on` | `off` disables the HTTP API |
| `AGENTBOARD_MODE` | `tui` | Startup mode: `tui`, `api`, `both` (only `tui` is implemented) |
| `AGENTBOARD_TMUX` | `auto` | tmux usage: `auto`, `always`, `never` |
| `AGENTBOARD_LLM_PROVIDER` | — | LLM provider for decomposition: `openai`, `anthropic`, `ollama` |
| `AGENTBOARD_LLM_MODEL` | — | Model name (e.g., `gpt-4o`, `claude-sonnet-4-20250514`) |
| `AGENTBOARD_LLM_API_KEY` | — | API key for LLM provider |
| `AGENTBOARD_LLM_BASE_URL` | — | Custom API base URL (for Ollama, etc.) |
| `AGENTBOARD_GIT_MERGE_STRATEGY` | `local` | Merge strategy: `local`, `pr` |
| `AGENTBOARD_GIT_WORKTREE_ROOT` | `~/.agentboard/worktrees` | Root dir for ticket worktrees |
| `AGENTBOARD_GIT_BRANCH_PREFIX` | `agent/` | Branch prefix for generated branches |
| `AGENTBOARD_GIT_BASE_BRANCH` | auto | Base branch for worktrees and merges |
| `AGENTBOARD_SESSION_ID` | set by board | Exported into each agent pane; the session the pane belongs to |
| `AGENTBOARD_LAUNCH_ID` | set by board | Exported into each agent pane; per-spawn generation id |
| `AGENTBOARD_HOOK_URL` | set by board | Exported into each agent pane; base URL of the hook receiver |
| `FREECODE_HOOK_URL` | set by board | Exported for freecode agents; per-session hook endpoint |
| `OPENCODE_CONFIG` | set by board | Exported for opencode agents; config that loads the board's reporting plugin |
| `AGENTBOARD_NPM_PATH` | `npm` | Path to npm binary (for MCP servers) |
| `AGENTBOARD_NODE_PATH` | `node` | Path to node binary (for MCP servers) |
| `NO_COLOR` | — | Disable colored output (respects standard env var) |

---

## Contributing

### Workflow

1. Create a feature branch from `main`
2. **Write the test first** — every feature, bugfix, or refactor starts with a failing test in the relevant `_test.go` file
3. Make the test pass with minimal implementation
4. Refactor if needed, keeping tests green
5. Run `go test ./...` and `go vet ./...`
6. Update AGENT.md if architecture changes
7. Submit PR with description linking to relevant ticket

### TDD Discipline

- **Red → Green → Refactor**. No implementation code without a test.
- Each `internal/` package has a `_test.go` file. Table-driven tests preferred.
- Tests must be independent and repeatable — no test ordering assumptions.
- Use interfaces and dependency injection to make packages testable in isolation.
- Mock external dependencies (tmux, MCP servers, LLM APIs) with test doubles.

### Code Conventions

- **Error wrapping**: Always wrap with `fmt.Errorf("package.context: %w", err)`
- **No circular imports**: `internal/` packages must not import each other circularly. Use interfaces defined at the consumer side.
- **No global state**: Pass dependencies explicitly. Use dependency injection in `cmd/agentboard/main.go`.
- **No comments unless asked**: Code should be self-documenting through naming.
- **Tests**: Each `internal/` package has a `_test.go` file. Table-driven tests preferred.
- **Go vet clean**: All code must pass `go vet`.

### Platform Support

- **Linux (x86_64, ARM64)**: Primary target
- **Termux (Android ARM64)**: Supported. No CGO dependencies. All pure Go.
- **macOS**: Best-effort. tmux integration works; PTY may differ.

### Git Conventions

- Conventional commits: `feat(tui): add kanban column reordering`
- Squash merge PRs
- Keep main green
