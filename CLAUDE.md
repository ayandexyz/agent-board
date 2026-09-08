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
│   ├── api/                # HTTP + WebSocket API server
│   │   ├── server.go       # chi router setup, server lifecycle
│   │   ├── handlers.go     # REST handlers (tickets, sessions, agents)
│   │   ├── websocket.go    # WebSocket hub and real-time event streaming
│   │   └── middleware.go   # CORS, logging, recovery middleware
│   ├── store/              # SQLite persistence
│   │   ├── sqlite.go       # DB connection, initialization, migrations
│   │   ├── tickets.go      # Ticket CRUD operations
│   │   ├── sessions.go     # Session and agent state persistence
│   │   └── migrations.go   # Schema migration definitions
│   ├── decomposition/      # LLM-powered project breakdown
│   │   ├── decomposer.go   # Project → tickets decomposition engine
│   │   ├── assigner.go     # Auto-assign tickets to agents with reasoning
│   │   └── prompts.go      # Prompt templates for decomposition and assignment
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
| **modernc.org/sqlite over CGO sqlite** | Pure Go, compiles on Termux (Android ARM64) without a C toolchain. |
| **chi over stdlib mux** | Lightweight, idiomatic, compatible with `net/http` handlers. Easy middleware stacking. |
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

# Run (API-only mode, for Next.js frontend)
./agentboard --api --addr :8080

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
id          TEXT PRIMARY KEY
ticket_id   TEXT FK → tickets.id
agent       TEXT
started_at  DATETIME
ended_at    DATETIME
status      TEXT  (running | completed | failed | cancelled)
context_key TEXT  (ContextCarry reference)
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

## Agent Activity Tracking (in progress)

Spawned agents report their own state over HTTP rather than the board guessing from the screen.

`internal/activity` is the receiver: a loopback `net/http` server on `127.0.0.1:0`, started in `runTUI` and handed to the orchestrator via `SetHookBaseURL`. It accepts `POST /hooks/{sessionID}` carrying Claude-Code-shaped hook JSON, maps it to a state (`active`, `idle`, `waiting_input`, `blocked`, `exited`), and fires one `notify-send` on entry into the needs-input family. `X-Agentboard-Launch` is checked against the launch id recorded at spawn, so a relaunched session's late hooks are dropped.

Not built yet: the store migration, the event hub, holding `PermissionRequest` open for a remote decision (the handler answers `{}` immediately and the pane picker takes over), the tool-flight rule beyond "any PostToolUse clears the block", the TUI glyph/waiting overlay, and the capture-pane reconciler.

Every agent pane is launched with its session identity exported via `tmux new-window -e`:

| Variable | Meaning |
|---|---|
| `AGENTBOARD_SESSION_ID` | The session this pane belongs to |
| `AGENTBOARD_LAUNCH_ID` | Per-spawn id; lets the board drop hooks from an earlier launch of the same session |
| `AGENTBOARD_HOOK_URL` | Base URL of the board's local hook receiver |
| `FREECODE_HOOK_URL` | FreeCode only: `<base>/hooks/<sessionID>` |

Claude Code takes no environment variable of its own. It is launched as
`claude --settings '<inline json>'`, built by `activity.ClaudeSettingsJSON`,
registering an `http` hook at `<base>/hooks/<sessionID>` for `SessionStart`,
`UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PermissionRequest`,
`Notification`, `Stop` and `SessionEnd`. The launch id rides along as the
`X-Agentboard-Launch` header, interpolated from `$AGENTBOARD_LAUNCH_ID` in the
pane — which Claude Code only does because `allowedEnvVars` lists it. Inline
settings keep the ticket worktree clean and cannot outlive the process.

Hooks block the turn, so lifecycle reports time out after 5 s; only
`PermissionRequest` waits (600 s), since that one is a human decision. Note the
two agents disagree on the reply shape: Claude Code wants
`decision: {"behavior": "allow"}` nested, FreeCode wants a bare
`decision: "allow"`. `PermissionRequest` from Claude Code carries no
`tool_use_id`.

The extra arguments reach the pane through `RunRequest.Args`, single-quoted into
the launch line by `agentCommand` in `internal/orchestrator/pty_runner.go`.

Nothing is exported unless `Service.SetHookBaseURL` was called, so an agent is never told to reach a receiver that is not running.

FreeCode support lives in the FreeCode repo at `apps/core/src/hooks/builtin/board-webhook.ts`, registered from `initHooks`. It posts Claude Code shaped hook payloads (`SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop`) so the board needs no FreeCode-specific mapper, and it answers permission requests remotely: a reply of `{"hookSpecificOutput":{"decision":"allow"}}` resolves the agent's pending prompt as a one-shot grant, `deny` rejects it, and no decision leaves the choice to the pane picker.

See `docs/superpowers/specs/2026-09-08-agent-activity-tracking-design.md` and `docs/superpowers/plans/2026-09-08-agent-activity-tracking.md`.

---

## Git Worktree Isolation & Merge Flow

Every ticket gets an isolated git worktree when an agent starts on it, so parallel agents never collide on the filesystem.

### Lifecycle

```
Backlog -> In Progress            -> Review              -> Done
           worktree + branch         merge gate            worktree removed,
           created, agent cwd        (modal in TUI)        branch deleted
```

- **Spawn** (`orchestrator.Service.prepareWorkDir`): `git worktree add ~/.agentboard/worktrees/<project>/<ticket-id> -b agent/<ticket-id> <base>` (branch from `ticket.Branch` if set), tmux window opens with `-c <worktree>`. Path persisted in `tickets.worktree_path`; `worktree.created` event recorded.
- **Review -> Done is gated**: `Service.MoveTicketStatus` refuses the transition with a typed `*MergeRequiredError` when unmerged work exists; the TUI turns that into a merge confirm modal (press `s` in the modal to switch strategy for this merge). `Service.MergeTicket(ctx, id, strategy)` performs the merge and moves the ticket to done itself.
- **Strategies** (`[git] merge_strategy`, default `local`):
  - `local`: rebase the ticket branch onto the base branch *inside the worktree*, then `git merge --ff-only` in the repo root (root must be clean and on the base branch). Conflicts abort the rebase, emit a `merge.conflict` event, and leave the ticket in review.
  - `pr`: push the branch and `gh pr create`; PR URL stored in `tickets.pr_url`. Moving to done requires the PR to report MERGED (`gh pr view --json state`), then cleanup runs with a forced branch delete (squash-safe).
- **Guardrails**: merges refuse while an agent session is active; dirty worktrees are never auto-removed (ticket delete also refuses); no git repo -> all of this degrades to the old behavior.

### Key packages

- `internal/git` — pure git/gh command wrappers over an injectable `Runner` closure (`ExecRunner()` in production, fakes in tests; no exec in unit tests).
- `internal/orchestrator/worktrees.go` — `WorktreeManager` (implements `Worktrees` + `Merger`).
- `internal/orchestrator/merge.go` — `MergeTicket`, `MoveTicketStatus`, `DeleteTicket`, `MergeRequiredError`.
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
| `m` | Merge ticket (Review only): opens the merge modal |
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
| `AGENTBOARD_ADDR` | `:8080` | API server bind address |
| `AGENTBOARD_MODE` | `tui` | Startup mode: `tui`, `api`, `both` |
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
