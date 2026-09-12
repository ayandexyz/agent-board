package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ayan-de/agent-board/internal/pty"
)

// ErrUnknownAgent is returned when a run names an agent the pty registry has
// no launch recipe for. The window is never created, so nothing leaks.
var ErrUnknownAgent = errors.New("orchestrator: unknown agent")

type tmuxAgentRunner struct {
	sessionName string
	mu          sync.Mutex
	panes       map[string]string
}

func NewTmuxAgentRunner(tmuxSession string) (*tmuxAgentRunner, error) {
	return &tmuxAgentRunner{
		sessionName: tmuxSession,
		panes:       map[string]string{},
	}, nil
}

func newWindowArgs(sessionName, windowName, workDir string, env []string) []string {
	args := []string{"new-window", "-t", sessionName, "-d", "-a", "-P", "-F", "#{window_id}:#{pane_id}", "-n", windowName}
	if workDir != "" {
		args = append(args, "-c", workDir)
	}
	// -e sets the variable for the new pane only, so each agent sees its own
	// session identity without touching the tmux server environment.
	for _, kv := range env {
		args = append(args, "-e", kv)
	}
	return args
}

// cacheDir is where the board keeps the scratch files it hands to agents:
// prompts, settings documents, the OpenCode plugin. Created on demand.
func cacheDir() string {
	homeDir, _ := os.UserHomeDir()
	dir := fmt.Sprintf("%s/.agentboard/cache", homeDir)
	_ = os.MkdirAll(dir, 0755)
	return dir
}

// launchArgs caches an agent's settings document and returns the arguments that
// point the agent at it. The document is a kilobyte of JSON; typed into the
// pane it would bury the launch line in the user's scrollback.
func launchArgs(cacheDir, sessionID, settings string) ([]string, error) {
	if settings == "" {
		return nil, nil
	}
	path := fmt.Sprintf("%s/settings-%s.json", cacheDir, sessionID)
	if err := os.WriteFile(path, []byte(settings), 0644); err != nil {
		return nil, fmt.Errorf("orchestrator.launchArgs: %w", err)
	}
	return []string{"--settings", path}, nil
}

// agentCommand is the line typed into the pane to launch the agent. Arguments
// are single quoted because one of them is an inline JSON document.
func agentCommand(bin string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, bin)
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			parts = append(parts, a)
			continue
		}
		parts = append(parts, "'"+strings.ReplaceAll(a, "'", "")+"'")
	}
	return strings.Join(parts, " ")
}

func (r *tmuxAgentRunner) Start(ctx context.Context, req RunRequest) (RunHandle, error) {
	cfg := pty.GetConfig(req.Agent)
	if cfg == nil {
		return RunHandle{}, fmt.Errorf("%w: %q", ErrUnknownAgent, req.Agent)
	}

	sessionName := r.sessionName
	if sessionName == "" {
		sessionName = "agentboard-agents"
	}

	windowName := fmt.Sprintf("agent-%s", shortID(req.SessionID, 8))

	cmd := exec.Command("tmux", newWindowArgs(sessionName, windowName, req.WorkDir, req.Env)...)
	output, err := cmd.Output()
	if err != nil {
		return RunHandle{}, fmt.Errorf("create tmux window: %w", err)
	}
	parts := strings.Split(strings.TrimSpace(string(output)), ":")
	paneID := parts[len(parts)-1]

	r.mu.Lock()
	r.panes[req.SessionID] = paneID
	r.mu.Unlock()

	_ = exec.Command("tmux", "set-option", "-p", "-t", paneID, "remain-on-exit", "on").Run()

	cache := cacheDir()
	promptFile := fmt.Sprintf("%s/prompt-%s.txt", cache, req.SessionID)
	if err := os.WriteFile(promptFile, []byte(req.Prompt), 0644); err != nil {
		return RunHandle{}, fmt.Errorf("write prompt file: %w", err)
	}

	args, err := launchArgs(cache, req.SessionID, req.Settings)
	if err != nil {
		return RunHandle{}, err
	}

	if err := exec.Command("tmux", "send-keys", "-t", paneID, agentCommand(cfg.Bin, args), "Enter").Run(); err != nil {
		return RunHandle{}, fmt.Errorf("send bin to pane: %w", err)
	}

	// One cancel covers both goroutines: a stopped run must neither keep
	// typing into the pane nor report a completion for an agent nobody is
	// waiting on.
	runCtx, cancel := context.WithCancel(ctx)
	go r.injectPrompt(runCtx, paneID, req.SessionID, cfg, promptFile)

	if req.Reporter != nil {
		req.Reporter(fmt.Sprintf("Agent %s started in tmux pane %s", req.Agent, paneID))
	}

	go r.monitorPane(runCtx, req.SessionID, paneID, cfg.Bin, req.OnComplete)

	return RunHandle{
		Outcome: "running",
		Summary: fmt.Sprintf("Agent %s in pane %s", req.Agent, paneID),
		Stop:    cancel,
	}, nil
}

func (r *tmuxAgentRunner) injectPrompt(ctx context.Context, paneID, sessionID string, cfg *pty.Config, promptFile string) {
	if err := waitForReady(ctx, paneID, cfg); err != nil {
		return
	}

	// The ready marker is a banner; the composer that takes the keystrokes can
	// be a beat behind it.
	if cfg.ReadyWait > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.ReadyWait):
		}
	}

	promptBytes, err := os.ReadFile(promptFile)
	if err != nil {
		return
	}
	prompt := string(promptBytes)

	for _, c := range prompt {
		if ctx.Err() != nil {
			return
		}
		if c == '\n' {
			if err := exec.Command("tmux", "send-keys", "-t", paneID, "-l", " ").Run(); err != nil {
				return
			}
		} else {
			if err := exec.Command("tmux", "send-keys", "-t", paneID, "-l", fmt.Sprintf("%c", c)).Run(); err != nil {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	exec.Command("tmux", "send-keys", "-t", paneID, "Enter").Run()
}

func waitForReady(ctx context.Context, paneID string, cfg *pty.Config) error {
	timeout := cfg.ReadyTimeout
	if timeout < 3*time.Second {
		timeout = 3 * time.Second
	}
	deadline := time.Now().Add(timeout)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		output, err := exec.Command("tmux", "capture-pane", "-t", paneID, "-p", "-J", "-S", "-200").Output()
		if err == nil {
			stripped := pty.StripANSI(string(output))
			if cfg.ReadyPattern != nil && cfg.ReadyPattern.MatchString(stripped) {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("agent did not become ready")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (r *tmuxAgentRunner) monitorPane(ctx context.Context, sessionID string, paneID string, agentBin string, onComplete func(outcome, summary, resumeCommand string)) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	defer r.forget(sessionID)

	var lastCaptured string
	sawAgentRunning := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Capture output continuously so we always have the latest content
			if out, err := r.capturePaneOutput(paneID, 5000); err == nil && out != "" {
				lastCaptured = out
			}

			checkCmd := exec.Command("tmux", "list-panes", "-t", paneID, "-F", "#{pane_pid}:#{pane_dead}:#{pane_current_command}")
			output, err := checkCmd.Output()

			outStr := strings.TrimSpace(string(output))
			fields := strings.SplitN(outStr, ":", 3)
			isDead := len(fields) > 1 && fields[1] == "1"
			currentCommand := ""
			if len(fields) > 2 {
				currentCommand = fields[2]
			}

			// tmux only marks a pane dead when its top-level process (the
			// wrapping shell) exits, not when the agent we launched inside
			// that shell exits back to a prompt. So once we've observed the
			// agent binary as the pane's foreground process, its absence
			// means the agent quit (e.g. via its own exit-and-print-resume
			// flow) even though the shell — and the pane — are still alive.
			if currentCommand == agentBin {
				sawAgentRunning = true
			}
			agentExited := sawAgentRunning && currentCommand != "" && currentCommand != agentBin

			if err != nil || len(outStr) == 0 || isDead || agentExited {
				outcome := "completed"
				summary := fmt.Sprintf("Agent session %s finished", sessionID)

				captured := captureFinalPaneOutput(lastCaptured, func() (string, error) {
					return r.capturePaneOutput(paneID, 5000)
				}, time.Sleep)

				if captured != "" {
					parsed, parseErr := parseOpencodeOutput(strings.NewReader(captured))
					if parseErr == nil {
						if parsed.Outcome != "" {
							outcome = parsed.Outcome
						}
						if parsed.Summary != "" {
							summary = parsed.Summary
						}
					}
				}

				if isDead || agentExited {
					_ = exec.Command("tmux", "kill-pane", "-t", paneID).Run()
				}

				if onComplete != nil {
					resumeCmd := ExtractResumeCommand(captured)
					onComplete(outcome, summary, resumeCmd)
				}
				return
			}
		}
	}
}

func (r *tmuxAgentRunner) capturePaneOutput(paneID string, lines int) (string, error) {
	output, err := captureTmuxPaneOutput("tmux", paneID, lines)
	if err != nil {
		return "", fmt.Errorf("failed to capture pane output: %w", err)
	}
	return output, nil
}

func (r *tmuxAgentRunner) GetPaneID(sessionID string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.panes[sessionID]
	return p, ok
}

// Kill tears down the pane a session runs in. A session the runner no longer
// knows is already gone, which is the outcome the caller wanted.
func (r *tmuxAgentRunner) Kill(sessionID string) error {
	paneID, ok := r.forget(sessionID)
	if !ok {
		return nil
	}
	if err := exec.Command("tmux", "kill-pane", "-t", paneID).Run(); err != nil {
		return fmt.Errorf("orchestrator.kill %s: %w", sessionID, err)
	}
	return nil
}

func (r *tmuxAgentRunner) forget(sessionID string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.panes[sessionID]
	delete(r.panes, sessionID)
	return p, ok
}

func shortID(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}
