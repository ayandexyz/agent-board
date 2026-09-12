package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ayan-de/agent-board/internal/activity"
	"github.com/ayan-de/agent-board/internal/api"
	"github.com/ayan-de/agent-board/internal/config"
	"github.com/ayan-de/agent-board/internal/git"
	"github.com/ayan-de/agent-board/internal/llm"
	"github.com/ayan-de/agent-board/internal/mcp"
	"github.com/ayan-de/agent-board/internal/orchestrator"
	"github.com/ayan-de/agent-board/internal/store"
	"github.com/ayan-de/agent-board/internal/theme"
	"github.com/ayan-de/agent-board/internal/tmux"
	"github.com/ayan-de/agent-board/internal/tui"
	"github.com/ayan-de/agent-board/internal/version"

	tea "github.com/charmbracelet/bubbletea"
)

// runTUI launches the Kanban TUI, re-executing inside a tmux session first
// if not already running in one. It returns the process exit code.
func runTUI() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		return 1
	}

	if !tmux.IsInTmux() {
		sessionName := cfg.ProjectName
		cmd := exec.Command("tmux", "new-session", "-A", "-s", sessionName, os.Args[0])
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "error launching tmux: %v\n", err)
			return 1
		}
		return 0
	}

	sessionName := cfg.ProjectName
	if actualSession, err := tmux.GetCurrentSessionName(); err == nil {
		sessionName = actualSession
	}

	statuses := make([]string, len(cfg.Board.Columns))
	for i, col := range cfg.Board.Columns {
		statuses[i] = col.Status
	}
	s, err := store.Open(cfg.DB.Path, statuses, cfg.Board.Prefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening store: %v\n", err)
		return 1
	}
	defer s.Close()

	llmClient, err := llm.NewFromConfig(cfg.LLM)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating llm client: %v\n", err)
		return 1
	}

	var runner orchestrator.Runner
	var agentRunner orchestrator.AgentRunner
	tmuxRunner, err := orchestrator.NewTmuxRunner(sessionName)
	if err == nil {
		runner = tmuxRunner
	}
	if tr, err := orchestrator.NewTmuxAgentRunner(sessionName); err == nil {
		agentRunner = tr
	}
	mcpManager := mcp.NewManager(cfg.MCP)
	ctxCarry := mcp.NewContextCarryAdapter(mcpManager, cfg.ProjectName)
	orch := orchestrator.NewService(s, llmClient, runner, ctxCarry)
	orch.SetBranchPrefix(cfg.Git.BranchPrefix)
	if agentRunner != nil {
		orch.SetAgentRunner(agentRunner)
	}

	var resolvePermission func(sessionID, decision string)
	var activityAlerts <-chan activity.Alert
	var activityReader tui.ActivityReader
	var apiActivity api.Activity
	recv := activity.New()
	recv.Label = orch.WindowForSession
	recv.Store = s
	if err := recv.Restore(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "warning: activity state not restored: %v\n", err)
	}
	if base, err := recv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: activity receiver unavailable: %v\n", err)
	} else {
		defer recv.Close()
		orch.SetHookBaseURL(base)
		orch.SetLaunchNotifier(recv.Launch)
		orch.SetActivityState(recv.StateOf)
		resolvePermission = func(sessionID, decision string) { recv.Resolve(sessionID, decision) }
		activityReader = recv
		apiActivity = recv
		// One subscription per consumer: the TUI's stream is its own, so an
		// API or push client added later takes nothing away from it.
		alerts, unsubscribe := recv.Subscribe()
		defer unsubscribe()
		activityAlerts = alerts
	}

	if cfg.General.API {
		apiServer := &api.Server{
			Addr:      cfg.General.Addr,
			Board:     orch,
			Activity:  apiActivity,
			Terminals: tmuxTerminals{session: sessionName},
			Identity: api.Identity{
				Name:        "agentboard",
				Version:     version.Version,
				Project:     cfg.ProjectName,
				TmuxSession: sessionName,
			},
		}
		if _, err := apiServer.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: api unavailable: %v\n", err)
		} else {
			defer apiServer.Close()
		}
	}

	g := git.New(git.ExecRunner())
	currentBranch := ""
	if cwd, err := os.Getwd(); err == nil {
		if repoRoot, err := g.RepoRoot(context.Background(), cwd); err == nil {
			if branch, err := g.CurrentBranch(context.Background(), repoRoot); err == nil {
				currentBranch = branch
			}
			wm := orchestrator.NewWorktreeManager(
				g,
				git.NewGH(git.ExecRunner()),
				repoRoot,
				cfg.Git.ResolvedWorktreeRoot(config.GetBaseDir(), cfg.ProjectName),
				cfg.Git.BranchPrefix,
				cfg.Git.BaseBranch,
			)
			orch.SetWorktrees(wm)
			orch.SetMergeConfig(orchestrator.MergeStrategy(cfg.Git.MergeStrategy))
		}
	}

	reg := theme.NewRegistry("dark")
	reg.LoadBuiltins()
	themesDir := filepath.Join(config.GetBaseDir(), "themes")
	reg.LoadUserThemes(themesDir)
	if err := reg.Set(cfg.TUI.Theme); err != nil {
		_ = reg.Set("agentboard")
	}

	app, err := tui.NewApp(cfg, reg, tui.AppDeps{
		Orchestrator:      orch,
		ActivityEvents:    activityAlerts,
		ResolvePermission: resolvePermission,
		Activity:          activityReader,
		CurrentBranch:     currentBranch,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating app: %v\n", err)
		return 1
	}
	defer app.Close()

	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error running tui: %v\n", err)
		return 1
	}
	return 0
}
