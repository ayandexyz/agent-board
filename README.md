
<div align="center">

```
                            █▀█ █▀▀ █▀▀ █▄░█ ▀█▀   █▄▄ █▀█ █▀█ █▀█ █▀▄
                            █▀█ █▄█ ██▄ █░▀█ ░█░   █▄█ █▄█ █▀█ █▀▄ █▄▀
```
<img src="docs/ChatGPT%20Image%20May%2014,%202026,%2001_13_21%20PM.png" width="280" height="200" />

**Opensource TUI based Kanban board for orchestrating AI coding agents**

[![Go Version](https://img.shields.io/github/go-mod/go-version/ayandexyz/agent-board)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Build Status](https://img.shields.io/badge/build-passing-brightgreen.svg)]()

</div>

**AgentBoard** is a terminal-based Kanban board for orchestrating and managing AI coding agents. It provides a visual development workflow for modern software engineering, bridging project management with automated code generation.

<img width="1863" height="450" alt="AgentBoard TUI Mockup" src="docs/image.png" />

---

## Key Features

- **Modern Kanban TUI**: Terminal UI built with [Bubble Tea](https://github.com/charmbracelet/bubbletea), featuring glassmorphism-inspired borders and smooth animations.
- **Agent Orchestration**: Spawn and manage agents like **Claude Code**, **OpenCode**, and **Cursor** via tmux panes or embedded PTY.
- **MCP Native**: Integrated Model Context Protocol support via `@thisisayande/contextcarry-mcp` for agent memory persistence.
- **Persistent Storage**: SQLite backend with automatic migrations.
- **AI Proposal Flow**: Coordinator LLM creates worker prompts, with approval gating before execution.
- **LangChain Go Integration**: Provider registry supporting OpenAI, Ollama, Claude, and Zai.

---

## 🌐 Marketing Website

For more information about AgentBoard, including feature showcases, screenshots, and project updates, visit the official website:

**https://agentboard.ayande.xyz/**

---

## Getting Started

### Prerequisites

- **Go**: 1.21 or higher
- **git**: For cloning the repository
- **tmux**: Required for tmux-mode agent spawning (Linux/macOS)
- **Node.js/npm**: Required for MCP server integrations

### Installation

#### From Binary (All Platforms)

Run the automated installer:

```bash
curl -sSL https://raw.githubusercontent.com/ayandexyz/agent-board/main/install.sh | bash
```

This will detect your OS and architecture, install dependencies, and place the binary in `~/.local/bin`.

#### From Source
Ensure [Go](https://go.dev/doc/install) 1.21+ is installed.

```bash
git clone https://github.com/ayandexyz/agent-board.git
cd agent-board
make run        # builds ./agentboard and starts it

# Or put it on your PATH (go install) and run it from any project
make install
agentboard
```

`make` on its own lists every target.

#### Platform-Specific Notes

| Platform | Package Manager | Install Command |
|----------|-----------------|-----------------|
| Linux (apt) | `apt-get install golang git tmux npm` | Binary or source |
| macOS (brew) | `brew install go git tmux npm node` | Binary or source |
| Android (Termux) | `pkg install golang git tmux nodejs` | Binary or source |

### Basic Usage

Start the interactive Kanban board:

```bash
./agentboard
```

Or if installed via binary:
```bash
agentboard
```

### CLI Commands

| Command | Description |
|---------|-------------|
| `agentboard` | Launch the Kanban TUI (default) |
| `agentboard update` | Check for and install the latest release in-place |
| `agentboard -v`, `--version`, `version` | Print the installed version |
| `agentboard -h`, `--help`, `help` | Show usage and available commands |

### Keybindings

| Key | Action |
|-----|--------|
| `h/l` or `←/→` | Move between Kanban columns |
| `j/k` or `↑/↓` | Move between tickets |
| `Enter` | Open ticket detail view |
| `a` | Add a new ticket in the active column |
| `d` | Delete selected ticket |
| `1-4` | Jump to a specific column |
| `?` | Toggle help view |
| `i` | Toggle agent dashboard |
| `:` | Open command palette |
| `q` | Quit with confirmation |
| `Esc` | Return to board from other views |

### Ticket View

| Key | Action |
|-----|--------|
| `j/k` or `↑/↓` | Move between fields |
| `e` | Edit the selected editable field |
| `s` | Cycle ticket status |
| `a` | Open agent selection |
| `p` | Approve pending proposal |
| `r` | Start approved run |
| `Esc` | Cancel edit or return to board |

### Dashboard

| Key | Action |
|-----|--------|
| `r` | Re-run agent detection |
| `Esc` | Return to board |

---

## Configuration

AgentBoard reads two TOML files, later ones overriding earlier ones, then environment variables on top:

1. `~/.agentboard/config.toml` (global, every project)
2. `~/.agentboard/projects/<project>/config.toml` (one project; `<project>` is the repo's folder name)
3. `AGENTBOARD_*` environment variables

Both files are created with commented defaults on first run. Settings that differ per repository, such as `[git] setup_script`, belong in the project file.

### Example

```toml
[general]
log = "info"
addr = "127.0.0.1:7878"

[board]
prefix = "APP-"

[llm]
provider = "openai"
model = "gpt-4o-mini"
api_key = "sk-..."

[git]
merge_strategy = "local"
copy_files = [".env", ".env.local"]
setup_script = "npm install"

[tui]
theme = "nord"
```

### All settings

#### `[general]`

| Key | Default | What it does |
|-----|---------|--------------|
| `log` | `"info"` | Log level: `debug`, `info`, `warn`, `error` |
| `api` | `true` | Start the local HTTP API alongside the TUI |
| `addr` | `"127.0.0.1:7878"` | Address the HTTP API listens on. Must be loopback; reach it remotely with `ssh -L` or `tailscale serve` |
| `mode` | `"tui"` | Startup mode. Only `tui` is implemented |
| `tmux` | `"auto"` | `auto`, `always`, `never` (reserved; agents currently always run in tmux) |

#### `[board]`

| Key | Default | What it does |
|-----|---------|--------------|
| `prefix` | first 3 letters of the project | Ticket ID prefix, e.g. `"APP-"` gives `APP-01` |
| `columns` | Backlog, In Progress, Review, Done | Ordered list of `{ status = "...", name = "..." }`. New tickets land in the first column |

#### `[agent]`

| Key | Default | What it does |
|-----|---------|--------------|
| `default` | `"opencode"` | Agent preselected when starting a ticket |

#### `[llm]`: ticket decomposition and summaries

| Key | Default | What it does |
|-----|---------|--------------|
| `provider` | — | `openai`, `claude`, `ollama`, `zai`, `minimax` |
| `model` | — | Model name for the provider |
| `api_key` | — | API key for the provider |
| `base_url` | — | Custom endpoint (Ollama, proxies, compatible APIs) |
| `coordinator_model` | `model` | Model used to plan and assign tickets |
| `summarizer_model` | `model` | Model used to summarise finished runs |
| `require_approval` | `true` | Ask before starting a run the coordinator proposed |

#### `[git]`: worktrees and merging

Every ticket in worktree mode gets its own git worktree and branch, so agents working in parallel never touch each other's files.

| Key | Default | What it does |
|-----|---------|--------------|
| `merge_strategy` | `"local"` | `local`: rebase the ticket branch and fast-forward the base branch. `pr`: push and open a PR with `gh` |
| `worktree_root` | `~/.agentboard/worktrees/<project>` | Where ticket worktrees are created |
| `branch_prefix` | `"agent/"` | Prefix for generated branch names, e.g. `agent/app-07` |
| `base_branch` | auto (`main`/`master`) | Branch worktrees start from and merge back into |
| `copy_files` | — | Files or globs, relative to the repo root, copied into each **new** worktree. Use it for gitignored files the agent needs, like `.env`. Missing files are skipped; paths outside the repo are refused |
| `setup_script` | — | Shell command run inside each **new** worktree after `copy_files` and before the agent starts, e.g. `npm install`, `go mod download`. If it fails, the worktree is removed and the ticket does not start; starting again retries from scratch. Time limit: 10 minutes |

Why `copy_files` and `setup_script` matter: git only puts tracked files in a worktree, so a fresh one has no `node_modules` and no `.env`. Without them, the agent's first `npm test` fails and it spends its turn installing or guessing at missing secrets. With

```toml
[git]
copy_files = [".env"]
setup_script = "npm install"
```

starting a ticket creates the worktree, copies `.env`, runs `npm install`, and only then starts the agent. A worktree that already exists (restarting an agent on the same ticket) is reused as-is.

#### `[tui]`

| Key | Default | What it does |
|-----|---------|--------------|
| `theme` | `"default"` | Color theme; see [Themes](#themes) |
| `layout` | `"compact"` | Board layout (reserved) |
| `[tui.keybindings]` | — | Remap keys, e.g. `start_agent = "s"`. The generated config lists every action name |

#### `[db]`

| Key | Default | What it does |
|-----|---------|--------------|
| `path` | `~/.agentboard/projects/<project>/board.db` | SQLite database for tickets and sessions |

#### `[mcp]`

| Key | Default | What it does |
|-----|---------|--------------|
| `npm_path` | `"npm"` | npm binary used to launch MCP servers |
| `node_path` | `"node"` | node binary used to launch MCP servers |
| `[mcp.<name>]` | `contextcarry` enabled | One table per MCP server: `enabled`, `command`, `args` |

### Environment variables

Each overrides the config key next to it.

| Variable | Config key |
|----------|------------|
| `AGENTBOARD_LOG` | `general.log` |
| `AGENTBOARD_API` | `general.api` (`off`, `false` or `0` disables) |
| `AGENTBOARD_ADDR` | `general.addr` |
| `AGENTBOARD_MODE` | `general.mode` |
| `AGENTBOARD_TMUX` | `general.tmux` |
| `AGENTBOARD_DB` | `db.path` |
| `AGENTBOARD_LLM_PROVIDER` | `llm.provider` |
| `AGENTBOARD_LLM_MODEL` | `llm.model` |
| `AGENTBOARD_LLM_API_KEY` | `llm.api_key` |
| `AGENTBOARD_LLM_BASE_URL` | `llm.base_url` |
| `AGENTBOARD_LLM_COORDINATOR_MODEL` | `llm.coordinator_model` |
| `AGENTBOARD_LLM_SUMMARIZER_MODEL` | `llm.summarizer_model` |
| `AGENTBOARD_GIT_MERGE_STRATEGY` | `git.merge_strategy` |
| `AGENTBOARD_GIT_WORKTREE_ROOT` | `git.worktree_root` |
| `AGENTBOARD_GIT_BRANCH_PREFIX` | `git.branch_prefix` |
| `AGENTBOARD_GIT_BASE_BRANCH` | `git.base_branch` |
| `AGENTBOARD_NPM_PATH` | `mcp.npm_path` |
| `AGENTBOARD_NODE_PATH` | `mcp.node_path` |

---

## Themes

AgentBoard ships with builtin themes. User themes can be placed in `~/.agentboard/themes/` or `<project>/.agentboard/themes/` as JSON files.

### Available Themes

| Theme | Description |
|-------|-------------|
| `agentboard` | Default AgentBoard theme |
| `catppuccin` | Soft, warm pastel |
| `dracula` | Dark with vibrant colors |
| `gruvbox` | Retro groove terminal |
| `matrix` | Cyberpunk green-on-black |
| `nord` | Clean, arctic palette |
| `tokyonight` | Tokyo night city lights |

---

## Architecture

```
agent-board/
├── cmd/agentboard/
│   └── main.go             # TUI entrypoint
├── internal/
│   ├── tui/                # Bubble Tea application
│   ├── store/              # SQLite persistence
│   ├── config/             # Config loading and agent detection
│   ├── theme/              # Theme registry
│   ├── keybinding/         # Keymap and action resolution
│   ├── llm/                # LangChain Go provider registry
│   ├── orchestrator/       # Agent lifecycle management
│   ├── pty/                # PTY agent configurations
│   ├── prompt/             # LLM prompt templates
│   ├── mcp/                # MCP manager and adapters
│   └── mcpclient/          # MCP stdio client wrapper
├── docs/                   # Design notes
└── AGENTS.md               # Project state documentation
```

---

## Development

### Prerequisites

- **Go**: 1.21+
- **tmux**: Required for agent spawning
- **Node.js/npm**: Required for MCP server integrations

### Commands

```bash
make run               # build and start
make build             # build ./agentboard
make install           # go install onto your PATH
make test              # unit tests
make test-integration  # integration tests against real git
make check             # vet + test, before a PR
```

### Debug Mode

```bash
AGENTBOARD_LOG=debug ./agentboard
```

3.  **Running Tests**:
    We follow a strict TDD discipline. Ensure all tests pass before submitting changes.
    ```bash
    make check
    ```

4.  **Database Migrations**:
    Migrations are handled automatically on startup, but you can inspect the schema in `internal/store/migrations.go`.

---

## 🤝 Contributing

We welcome contributions! Please follow our workflow to keep the board clean and efficient:

1.  **Fork** the repository and create your feature branch.
2.  **Write Tests First**: We strictly follow Red-Green-Refactor. No implementation without a corresponding `_test.go` file.
3.  **Lint your code**: Run `go vet ./...` to ensure idiomatic Go.
4.  **Document**: Update `AGENT.md` if you introduce architectural changes.
5.  **Submit PR**: Ensure your commit messages follow [Conventional Commits](https://www.conventionalcommits.org/).

### Architecture Overview

- `cmd/`: Application entrypoints.
- `internal/tui/`: Bubble Tea models and components.
- `internal/orchestrator/`: Agent lifecycle and process management.
- `internal/store/`: SQLite persistence layer.
- `internal/mcp/`: Protocol clients for context management.

---

## 📜 License

Distributed under the MIT License. See `LICENSE` for more information.

---

<p align="center">
  Built with ❤️ for the AI-First Engineering community.
</p>

---

## Contributing

1. Fork and create a feature branch from `main`
2. Write tests first (TDD discipline)
3. Make tests pass
4. Refactor while keeping tests green
5. Run `make check` (`go vet ./...` and `go test ./...`)
6. Update `AGENTS.md` for architectural changes
7. Submit a PR with [Conventional Commits](https://www.conventionalcommits.org/)

---

## License

MIT License. See `LICENSE` for details.