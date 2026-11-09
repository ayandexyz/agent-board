# Agent Activity Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Track every spawned agent's activity state concurrently, show "needs you" on the board and as a desktop notification, and let the user approve a permission request or answer from the TUI.

**Architecture:** A new `internal/activity` package holds a pure state machine, an in-process hub, a loopback HTTP receiver for Claude Code hooks, a notifier, and a capture-pane reconciler. The orchestrator passes session id, launch id, and hook URL into each spawned agent and gains cancellable monitors. The TUI subscribes to the hub for a status glyph per card, a waiting count in the header, and a waiting overlay that resolves requests.

**Tech Stack:** Go 1.26, stdlib `net/http`, `database/sql` with `modernc.org/sqlite`, Bubble Tea and lipgloss for the TUI, `tmux` and `notify-send` via `os/exec` behind injectable runners. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-08-agent-activity-tracking-design.md`

## Global Constraints

- Every feature starts with a failing test. `go test ./...` and `go vet ./...` must be green at every commit.
- Error wrapping: `fmt.Errorf("package.context: %w", err)`.
- No new module dependencies. Pure Go only, must compile on Termux.
- No `exec` in unit tests: tmux and notify-send go behind injectable function types, or through the fake-tmux-on-PATH harness already used in `internal/orchestrator/pty_runner_test.go`.
- Interfaces are defined where consumed. `internal/activity` must not import `internal/orchestrator` or `internal/tui`.
- Conventional commit messages: `feat(activity): ...`, `fix(orchestrator): ...`.
- Activity states are exactly: `active`, `idle`, `waiting_input`, `blocked`, `exited`. TUI also renders a derived `no_signal`.
- Reconciler may never write `blocked` and may never clear a sticky state.
- Notifications fire only on entry into the needs-input family.
- This plan covers spec phases 1 to 4 and phase 6 (FreeCode). Phase 5 (OpenCode) shipped separately as a board-supplied OpenCode plugin. Phases 7 and 8 (Cursor and Codex mappers, WebSocket) are separate plans.
- FreeCode tasks edit the sibling repo at `/home/ayande/Project/freecode` (TypeScript, pnpm, `node:test` via `pnpm --filter @freecode/core test`). Commit there separately with the same conventional-commit style.

---

## File Structure

Created:

| File | Responsibility |
|---|---|
| `internal/activity/state.go` | `State` type, constants, `NeedsInput`, `IsSticky`. |
| `internal/activity/signal.go` | `Signal` struct that every mapper produces. |
| `internal/activity/machine.go` | Pure transition function plus `ToolFlight` bookkeeping. |
| `internal/activity/claude.go` | Claude Code hook payload to `Signal`. Also builds the `--settings` JSON. |
| `internal/activity/hub.go` | In-process pub/sub of `Event`. |
| `internal/activity/service.go` | Applies signals against the store, holds pending requests, resolves them. |
| `internal/activity/receiver.go` | `http.Handler` for `POST /hooks/{sessionID}`. |
| `internal/activity/notify.go` | Hub subscriber that calls `notify-send` on entry into needs-input. |
| `internal/activity/screen.go` | Structural classifier over captured pane text. |
| `internal/activity/reconcile.go` | Periodic stale-state reconciler. |
| `internal/tui/waiting_overlay.go` | List of sessions needing input, resolve keys, jump key. |

Modified:

| File | Change |
|---|---|
| `internal/store/migrations.go` | `sessions.activity_state`, `sessions.activity_at`, `sessions.launch_id`; `pending_requests` table. |
| `internal/store/sessions.go` | `SetActivity`, `ListActivity`, `SessionActivity` type. |
| `internal/store/requests.go` (new) | `PendingRequest` CRUD. |
| `internal/orchestrator/types.go` | `RunRequest.Env`, `RunRequest.ExtraArgs`, `RunHandle.Stop`, `AgentRunner.GetPaneID` becomes real. |
| `internal/orchestrator/pty_runner.go` | Bug fixes 1, 2, 3; env and args injection; pane map; chunked prompt. |
| `internal/orchestrator/service.go` | Bug fix 4; activity wiring; `StopSession` cancels; `SendInput` goes through the guard. |
| `internal/tui/app.go` | Hub subscription, header count, overlay wiring, `w` key. |
| `internal/tui/ticketcard.go` | Activity glyph in footer. |
| `internal/keybinding/action.go`, `keymap.go` | `ActionShowWaiting` bound to `w`. |
| `cmd/agentboard/run.go` | Construct activity service, receiver, notifier, reconciler. |
| `CLAUDE.md` | Document the activity subsystem and the `w` key. |

FreeCode repo (`/home/ayande/Project/freecode`):

| File | Change |
|---|---|
| `apps/core/src/hooks/builtin/board-webhook.ts` (new) | Pure payload builders plus `registerBoardWebhook()`. |
| `apps/core/src/hooks/builtin/board-webhook.test.ts` (new) | Tests for the pure builders and the decision mapping. |
| `apps/core/src/hooks/bootstrap.ts` | Call `registerBoardWebhook()` in `initHooks`. |
| `apps/core/src/agent/loop.ts:2440-2444` | Add `toolUseId: toolCall.id` to the hook context. |

---

## Phase 1: Blocking bug fixes

### Task 1: Return an error for unknown agents instead of panicking

**Files:**
- Modify: `internal/orchestrator/pty_runner.go:50-66`
- Test: `internal/orchestrator/pty_runner_test.go`

**Interfaces:**
- Produces: `tmuxAgentRunner.Start` returns `error` wrapping `ErrUnknownAgent` when `pty.GetConfig` is nil.

- [ ] **Step 1: Write the failing test**

Append to `internal/orchestrator/pty_runner_test.go`:

```go
func TestPtyRunnerStartRejectsUnknownAgent(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-test,123,0")
	t.Setenv("HOME", t.TempDir())
	tmuxDir := t.TempDir()
	t.Setenv("FAKE_TMUX_LOG", filepath.Join(tmuxDir, "tmux.log"))
	t.Setenv("PATH", tmuxDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	fakeTmux := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$FAKE_TMUX_LOG\"\ncase \"$1\" in new-window) printf '%%42\\n';; esac\n"
	if err := os.WriteFile(filepath.Join(tmuxDir, "tmux"), []byte(fakeTmux), 0755); err != nil {
		t.Fatal(err)
	}

	runner, _ := NewTmuxAgentRunner("agentboard")
	_, err := runner.Start(context.Background(), RunRequest{SessionID: "SES-01", Agent: "cursor", Prompt: "x"})
	if !errors.Is(err, ErrUnknownAgent) {
		t.Fatalf("Start() error = %v, want ErrUnknownAgent", err)
	}
}
```

Add `"errors"` to the test file imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/orchestrator -run TestPtyRunnerStartRejectsUnknownAgent -v`
Expected: build error `undefined: ErrUnknownAgent`, or a panic from the nil dereference.

- [ ] **Step 3: Write minimal implementation**

In `internal/orchestrator/pty_runner.go`, add near the top:

```go
var ErrUnknownAgent = errors.New("orchestrator: unknown agent")
```

Add `"errors"` to imports. Move the config lookup before the window is created and guard it:

```go
func (r *tmuxAgentRunner) Start(ctx context.Context, req RunRequest) (RunHandle, error) {
	cfg := pty.GetConfig(req.Agent)
	if cfg == nil {
		return RunHandle{}, fmt.Errorf("%w: %q", ErrUnknownAgent, req.Agent)
	}
	sessionName := r.sessionName
	...
```

Delete the later `cfg := pty.GetConfig(req.Agent)` line and simplify the `FormatPrompt` guard to `if cfg.FormatPrompt != nil`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/orchestrator -run TestPtyRunnerStart -v`
Expected: PASS for the new test and the existing Start tests.

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/pty_runner.go internal/orchestrator/pty_runner_test.go
git commit -m "fix(orchestrator): reject unknown agents instead of panicking"
```

### Task 2: Cancellable pane monitor

**Files:**
- Modify: `internal/orchestrator/types.go:60-64` (RunHandle)
- Modify: `internal/orchestrator/pty_runner.go:32-79,130-200`
- Test: `internal/orchestrator/pty_runner_test.go`

**Interfaces:**
- Produces: `RunHandle.Stop func()` that cancels the monitor and prompt-injection goroutines. `tmuxAgentRunner.monitorPane(ctx context.Context, ...)`.

- [ ] **Step 1: Write the failing test**

```go
func TestPtyRunnerStopCancelsMonitor(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-test,123,0")
	t.Setenv("HOME", t.TempDir())
	tmuxDir := t.TempDir()
	logFile := filepath.Join(tmuxDir, "tmux.log")
	t.Setenv("FAKE_TMUX_LOG", logFile)
	t.Setenv("PATH", tmuxDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	fakeTmux := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_TMUX_LOG"
case "$1" in
  new-window) printf '%%42\n' ;;
  capture-pane) printf 'Ask anything\n' ;;
  list-panes) printf '123:0:opencode\n' ;;
esac
`
	if err := os.WriteFile(filepath.Join(tmuxDir, "tmux"), []byte(fakeTmux), 0755); err != nil {
		t.Fatal(err)
	}

	completed := make(chan struct{}, 1)
	runner, _ := NewTmuxAgentRunner("agentboard")
	handle, err := runner.Start(context.Background(), RunRequest{
		SessionID:  "SES-02",
		Agent:      "opencode",
		Prompt:     "x",
		OnComplete: func(string, string, string) { completed <- struct{}{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if handle.Stop == nil {
		t.Fatal("RunHandle.Stop is nil")
	}
	handle.Stop()
	time.Sleep(1200 * time.Millisecond)

	before, _ := os.ReadFile(logFile)
	time.Sleep(1200 * time.Millisecond)
	after, _ := os.ReadFile(logFile)
	if len(after) != len(before) {
		t.Fatalf("monitor kept running after Stop: log grew from %d to %d bytes", len(before), len(after))
	}
	select {
	case <-completed:
		t.Fatal("OnComplete fired after Stop")
	default:
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/orchestrator -run TestPtyRunnerStopCancelsMonitor -v`
Expected: FAIL, `RunHandle.Stop is nil` (compile error on the field first).

- [ ] **Step 3: Write minimal implementation**

In `internal/orchestrator/types.go` add to `RunHandle`:

```go
type RunHandle struct {
	Outcome       string
	Summary       string
	ResumeCommand string
	Stop          func()
}
```

In `pty_runner.go`, inside `Start` after the pane is created:

```go
	runCtx, cancel := context.WithCancel(context.Background())
	go r.injectPrompt(runCtx, paneID, req.SessionID, cfg, promptFile)
	...
	go r.monitorPane(runCtx, req.SessionID, paneID, cfg.Bin, req.OnComplete)

	return RunHandle{
		Outcome: "running",
		Summary: fmt.Sprintf("Agent %s in pane %s", req.Agent, paneID),
		Stop:    cancel,
	}, nil
```

Change `monitorPane` signature to `monitorPane(ctx context.Context, sessionID, paneID, agentBin string, onComplete func(outcome, summary, resumeCommand string))` and replace `case <-context.Background().Done():` with `case <-ctx.Done():`. Change `injectPrompt` to take `ctx` first and check `ctx.Err() != nil` at the top of the per-rune loop, returning early. `waitForReady` takes `ctx` and returns `ctx.Err()` when cancelled inside its poll loop.

The `runCtx` deliberately derives from `context.Background()`, not the request ctx, because the caller's ctx is a short request scope and the monitor must outlive it.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/orchestrator -count=3 -v 2>&1 | tail -20`
Expected: PASS. The three previously order-dependent tests now pass in every run because no goroutines leak between tests.

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/types.go internal/orchestrator/pty_runner.go internal/orchestrator/pty_runner_test.go
git commit -m "fix(orchestrator): make pane monitor cancellable via RunHandle.Stop"
```

### Task 3: StopSession actually stops the agent and pane lookup works

**Files:**
- Modify: `internal/orchestrator/pty_runner.go` (pane map, `GetPaneID`, `Kill`)
- Modify: `internal/orchestrator/service.go:377-396` (`StopSession`), `AgentSession` struct
- Modify: `internal/orchestrator/types.go` (`AgentRunner` interface)
- Test: `internal/orchestrator/pty_runner_test.go`, `internal/orchestrator/service_test.go`

**Interfaces:**
- Produces: `AgentRunner.Kill(sessionID string) error` sends `tmux kill-pane`. `AgentRunner.GetPaneID(sessionID) (string, bool)` returns the stored pane. `AgentSession.stop func()` stored by the service.

- [ ] **Step 1: Write the failing tests**

In `pty_runner_test.go`:

```go
func TestPtyRunnerGetPaneIDAndKill(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-test,123,0")
	t.Setenv("HOME", t.TempDir())
	tmuxDir := t.TempDir()
	logFile := filepath.Join(tmuxDir, "tmux.log")
	t.Setenv("FAKE_TMUX_LOG", logFile)
	t.Setenv("PATH", tmuxDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	fakeTmux := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$FAKE_TMUX_LOG\"\ncase \"$1\" in new-window) printf '@7:%%42\\n';; capture-pane) printf 'Ask anything\\n';; list-panes) printf '1:0:opencode\\n';; esac\n"
	if err := os.WriteFile(filepath.Join(tmuxDir, "tmux"), []byte(fakeTmux), 0755); err != nil {
		t.Fatal(err)
	}
	runner, _ := NewTmuxAgentRunner("agentboard")
	handle, err := runner.Start(context.Background(), RunRequest{SessionID: "SES-03", Agent: "opencode", Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Stop()

	pane, ok := runner.GetPaneID("SES-03")
	if !ok || pane != "%42" {
		t.Fatalf("GetPaneID = %q,%v want %%42,true", pane, ok)
	}
	if err := runner.Kill("SES-03"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(logFile)
	if !strings.Contains(string(raw), "kill-pane -t %42") {
		t.Fatalf("expected kill-pane in log, got:\n%s", raw)
	}
	if _, ok := runner.GetPaneID("SES-03"); ok {
		t.Fatal("pane still registered after Kill")
	}
}
```

In `service_test.go`, add a fake runner and a test:

```go
type fakeAgentRunner struct {
	stopped bool
	killed  string
}

func (f *fakeAgentRunner) Start(_ context.Context, _ RunRequest) (RunHandle, error) {
	return RunHandle{Outcome: "running", Stop: func() { f.stopped = true }}, nil
}
func (f *fakeAgentRunner) GetPaneID(string) (string, bool) { return "%1", true }
func (f *fakeAgentRunner) Kill(id string) error             { f.killed = id; return nil }

func TestStopSessionCancelsMonitorAndKillsPane(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	fr := &fakeAgentRunner{}
	svc := NewService(fs, nil, nil, nil)
	svc.SetAgentRunner(fr)

	sess, err := svc.StartAdHocRun(context.Background(), "AGT-01", "opencode", "do it")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.StopSession(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if !fr.stopped {
		t.Fatal("monitor was not cancelled")
	}
	if fr.killed != sess.ID {
		t.Fatalf("Kill called with %q, want %q", fr.killed, sess.ID)
	}
	if fs.lastAgentActive {
		t.Fatal("ticket still agent_active after StopSession")
	}
}
```

Check `fakeStore.CreateSession` in `helpers_test.go` assigns an ID; if it does not, set `s.ID = "SES-01"` there.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/orchestrator -run 'TestPtyRunnerGetPaneIDAndKill|TestStopSessionCancelsMonitorAndKillsPane' -v`
Expected: compile errors for `Kill`.

- [ ] **Step 3: Write minimal implementation**

`pty_runner.go`:

```go
type tmuxAgentRunner struct {
	sessionName string
	mu          sync.Mutex
	panes       map[string]string
}

func NewTmuxAgentRunner(tmuxSession string) (*tmuxAgentRunner, error) {
	return &tmuxAgentRunner{sessionName: tmuxSession, panes: map[string]string{}}, nil
}
```

In `Start` after `paneID` is parsed:

```go
	r.mu.Lock()
	r.panes[req.SessionID] = paneID
	r.mu.Unlock()
```

Replace the `GetPaneID` stub and add `Kill`:

```go
func (r *tmuxAgentRunner) GetPaneID(sessionID string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.panes[sessionID]
	return p, ok
}

func (r *tmuxAgentRunner) Kill(sessionID string) error {
	r.mu.Lock()
	paneID, ok := r.panes[sessionID]
	delete(r.panes, sessionID)
	r.mu.Unlock()
	if !ok {
		return nil
	}
	if err := exec.Command("tmux", "kill-pane", "-t", paneID).Run(); err != nil {
		return fmt.Errorf("orchestrator.kill %s: %w", sessionID, err)
	}
	return nil
}
```

In `monitorPane`, when it returns after completion, also delete the pane from the map (wrap in a small `r.forget(sessionID)` helper).

`types.go`:

```go
type AgentRunner interface {
	Start(ctx context.Context, req RunRequest) (RunHandle, error)
	GetPaneID(sessionID string) (string, bool)
	Kill(sessionID string) error
}
```

`service.go`: add `stop func()` to `AgentSession` (unexported field). In `StartAdHocRun` and `StartApprovedRun`, set `stop: handle.Stop` when registering in `activeSessions`. Replace the body of `StopSession`:

```go
func (s *Service) StopSession(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	sess, ok := s.activeSessions[sessionID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("session %s not found", sessionID)
	}
	delete(s.activeSessions, sessionID)
	s.mu.Unlock()

	if sess.stop != nil {
		sess.stop()
	}
	if s.agentRunner != nil {
		_ = s.agentRunner.Kill(sessionID)
	}
	_ = s.store.EndSession(ctx, sessionID, "cancelled")
	if sess.TicketID != "" {
		_ = s.store.SetAgentActive(ctx, sess.TicketID, false)
	}
	return nil
}
```

Simplify `SwitchToPane` to use `s.agentRunner.GetPaneID` first and run `tmux select-window -t <paneID>` (select-window accepts a pane target and switches the visible window).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/orchestrator -v 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator
git commit -m "fix(orchestrator): StopSession cancels monitor and kills pane; real GetPaneID"
```

### Task 4: Undo agent_active when no runner is configured

**Files:**
- Modify: `internal/orchestrator/service.go:148-150,303-305`
- Test: `internal/orchestrator/service_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestStartAdHocRunWithoutRunnerClearsAgentActive(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	svc := NewService(fs, nil, nil, nil)
	_, err := svc.StartAdHocRun(context.Background(), "AGT-01", "opencode", "x")
	if err == nil {
		t.Fatal("expected error")
	}
	if fs.lastAgentActive {
		t.Fatal("agent_active left true after failed start")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/orchestrator -run TestStartAdHocRunWithoutRunnerClearsAgentActive -v`
Expected: FAIL, `agent_active left true`.

- [ ] **Step 3: Write minimal implementation**

In both `StartAdHocRun` and `StartApprovedRun`, replace the `else { return ..., fmt.Errorf("no runner configured") }` with:

```go
	} else {
		err = fmt.Errorf("orchestrator.start: no runner configured")
	}
```

so control falls through to the existing `if err != nil` block that ends the session and clears the flag.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/orchestrator -v 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/service.go internal/orchestrator/service_test.go
git commit -m "fix(orchestrator): clear agent_active when no runner is configured"
```

---

## Phase 2: Activity core

### Task 5: State type and predicates

**Files:**
- Create: `internal/activity/state.go`
- Test: `internal/activity/state_test.go`

**Interfaces:**
- Produces:

```go
package activity

type State string

const (
	Active       State = "active"
	Idle         State = "idle"
	WaitingInput State = "waiting_input"
	Blocked      State = "blocked"
	Exited       State = "exited"
)

func (s State) NeedsInput() bool
func (s State) IsSticky() bool
func (s State) Valid() bool
```

- [ ] **Step 1: Write the failing test**

```go
package activity

import "testing"

func TestStatePredicates(t *testing.T) {
	cases := []struct {
		s          State
		needsInput bool
		sticky     bool
		valid      bool
	}{
		{Active, false, false, true},
		{Idle, false, false, true},
		{WaitingInput, true, true, true},
		{Blocked, true, true, true},
		{Exited, false, false, true},
		{State("bogus"), false, false, false},
	}
	for _, c := range cases {
		if got := c.s.NeedsInput(); got != c.needsInput {
			t.Errorf("%s.NeedsInput() = %v", c.s, got)
		}
		if got := c.s.IsSticky(); got != c.sticky {
			t.Errorf("%s.IsSticky() = %v", c.s, got)
		}
		if got := c.s.Valid(); got != c.valid {
			t.Errorf("%s.Valid() = %v", c.s, got)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/activity -v`
Expected: build failure, package has no non-test files.

- [ ] **Step 3: Write minimal implementation**

```go
package activity

type State string

const (
	Active       State = "active"
	Idle         State = "idle"
	WaitingInput State = "waiting_input"
	Blocked      State = "blocked"
	Exited       State = "exited"
)

func (s State) NeedsInput() bool { return s == WaitingInput || s == Blocked }

func (s State) IsSticky() bool { return s.NeedsInput() }

func (s State) Valid() bool {
	switch s {
	case Active, Idle, WaitingInput, Blocked, Exited:
		return true
	}
	return false
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/activity -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/activity
git commit -m "feat(activity): state type and predicates"
```

### Task 6: Signal and the transition machine with tool-flight precedence

**Files:**
- Create: `internal/activity/signal.go`, `internal/activity/machine.go`
- Test: `internal/activity/machine_test.go`

**Interfaces:**
- Produces:

```go
type Signal struct {
	Event     string // "session-start","user-prompt-submit","pre-tool-use","post-tool-use","permission-request","notification","stop","session-end","process-exited","terminal-idle","terminal-active","terminal-waiting"
	State     State  // proposed state; empty means "no state change, bookkeeping only"
	ToolName  string
	ToolUseID string
	LaunchID  string
}

type ToolFlight struct{ ... } // zero value ready

// Apply returns the next state and whether the signal was accepted.
// A rejected signal still updates flight bookkeeping.
func Apply(cur State, flight *ToolFlight, sig Signal) (next State, accepted bool)

func IsTurnBoundary(event string) bool
```

- [ ] **Step 1: Write the failing test**

```go
package activity

import "testing"

func TestApplyBasicTransitions(t *testing.T) {
	cases := []struct {
		name string
		cur  State
		sig  Signal
		want State
		ok   bool
	}{
		{"prompt submit activates", Idle, Signal{Event: "user-prompt-submit", State: Active}, Active, true},
		{"stop idles", Active, Signal{Event: "stop", State: Idle}, Idle, true},
		{"permission blocks", Active, Signal{Event: "permission-request", State: Blocked, ToolName: "Bash", ToolUseID: "t1"}, Blocked, true},
		{"session end exits", Idle, Signal{Event: "session-end", State: Exited}, Exited, true},
		{"empty state is bookkeeping only", Active, Signal{Event: "pre-tool-use", ToolName: "Read", ToolUseID: "t9"}, Active, false},
		{"exited only revives on prompt submit", Exited, Signal{Event: "stop", State: Idle}, Exited, false},
		{"exited revives on prompt submit", Exited, Signal{Event: "user-prompt-submit", State: Active}, Active, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &ToolFlight{}
			got, ok := Apply(c.cur, f, c.sig)
			if got != c.want || ok != c.ok {
				t.Fatalf("Apply = %s,%v want %s,%v", got, ok, c.want, c.ok)
			}
		})
	}
}

func TestBlockedClearsOnlyForBlockingToolPost(t *testing.T) {
	f := &ToolFlight{}
	Apply(Active, f, Signal{Event: "pre-tool-use", ToolName: "Bash", ToolUseID: "t1", State: Active})
	Apply(Active, f, Signal{Event: "pre-tool-use", ToolName: "Read", ToolUseID: "t2", State: Active})
	cur, _ := Apply(Active, f, Signal{Event: "permission-request", ToolName: "Bash", ToolUseID: "t1", State: Blocked})
	if cur != Blocked {
		t.Fatal("expected blocked")
	}

	next, ok := Apply(cur, f, Signal{Event: "post-tool-use", ToolName: "Read", ToolUseID: "t2", State: Active})
	if ok || next != Blocked {
		t.Fatalf("subagent post cleared block: %s,%v", next, ok)
	}

	next, ok = Apply(cur, f, Signal{Event: "post-tool-use", ToolName: "Bash", ToolUseID: "t1", State: Active})
	if !ok || next != Active {
		t.Fatalf("blocking tool post did not clear: %s,%v", next, ok)
	}
}

func TestBlockedAmbiguousToolNameFailsClosed(t *testing.T) {
	f := &ToolFlight{}
	Apply(Active, f, Signal{Event: "pre-tool-use", ToolName: "Bash", ToolUseID: "a", State: Active})
	Apply(Active, f, Signal{Event: "pre-tool-use", ToolName: "Bash", ToolUseID: "b", State: Active})
	cur, _ := Apply(Active, f, Signal{Event: "permission-request", ToolName: "Bash", State: Blocked})

	next, ok := Apply(cur, f, Signal{Event: "post-tool-use", ToolName: "Bash", ToolUseID: "a", State: Active})
	if ok || next != Blocked {
		t.Fatalf("ambiguous candidate cleared block: %s,%v", next, ok)
	}
	next, ok = Apply(cur, f, Signal{Event: "stop", State: Idle})
	if !ok || next != Idle {
		t.Fatalf("turn boundary did not clear: %s,%v", next, ok)
	}
}

func TestStickyIgnoresToolTraffic(t *testing.T) {
	f := &ToolFlight{}
	next, ok := Apply(WaitingInput, f, Signal{Event: "post-tool-use", ToolName: "Read", ToolUseID: "z", State: Active})
	if ok || next != WaitingInput {
		t.Fatalf("tool traffic cleared waiting_input: %s,%v", next, ok)
	}
}

func TestTerminalSignalsNeverTouchSticky(t *testing.T) {
	f := &ToolFlight{}
	for _, cur := range []State{WaitingInput, Blocked} {
		next, ok := Apply(cur, f, Signal{Event: "terminal-idle", State: Idle})
		if ok || next != cur {
			t.Fatalf("terminal signal demoted %s to %s", cur, next)
		}
	}
	next, ok := Apply(Active, f, Signal{Event: "terminal-idle", State: Idle})
	if !ok || next != Idle {
		t.Fatal("terminal-idle should demote active")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/activity -v`
Expected: compile errors, `Signal` and `Apply` undefined.

- [ ] **Step 3: Write minimal implementation**

`signal.go`:

```go
package activity

type Signal struct {
	Event     string
	State     State
	ToolName  string
	ToolUseID string
	LaunchID  string
}
```

`machine.go`:

```go
package activity

const maxInflightTools = 128

type ToolFlight struct {
	inflight  map[string]string
	candidate string
}

func (f *ToolFlight) track(sig Signal) {
	if f.inflight == nil {
		f.inflight = map[string]string{}
	}
	switch sig.Event {
	case "pre-tool-use":
		if len(f.inflight) >= maxInflightTools {
			f.inflight = map[string]string{}
		}
		if sig.ToolUseID != "" {
			f.inflight[sig.ToolUseID] = sig.ToolName
		}
	case "post-tool-use":
		delete(f.inflight, sig.ToolUseID)
	}
	if IsTurnBoundary(sig.Event) {
		f.inflight = map[string]string{}
		f.candidate = ""
	}
}

func (f *ToolFlight) chooseCandidate(sig Signal) {
	if sig.ToolUseID != "" {
		if _, ok := f.inflight[sig.ToolUseID]; ok {
			f.candidate = sig.ToolUseID
			return
		}
	}
	match := ""
	for id, name := range f.inflight {
		if name == sig.ToolName {
			if match != "" {
				f.candidate = ""
				return
			}
			match = id
		}
	}
	f.candidate = match
}

func IsTurnBoundary(event string) bool {
	switch event {
	case "user-prompt-submit", "stop", "session-end", "process-exited":
		return true
	}
	return false
}

func isToolEvent(event string) bool {
	return event == "pre-tool-use" || event == "post-tool-use"
}

func isTerminalEvent(event string) bool {
	return len(event) > 9 && event[:9] == "terminal-"
}

func Apply(cur State, flight *ToolFlight, sig Signal) (State, bool) {
	postOfCandidate := sig.Event == "post-tool-use" && flight.candidate != "" && sig.ToolUseID == flight.candidate
	flight.track(sig)

	if sig.State == "" {
		return cur, false
	}
	if cur == Exited && sig.Event != "user-prompt-submit" {
		return cur, false
	}
	if isTerminalEvent(sig.Event) && cur.IsSticky() {
		return cur, false
	}
	if sig.State == Blocked && sig.Event == "permission-request" {
		flight.chooseCandidate(sig)
		return Blocked, true
	}
	if cur == Blocked {
		if IsTurnBoundary(sig.Event) || postOfCandidate {
			flight.candidate = ""
			return sig.State, true
		}
		return cur, false
	}
	if cur.IsSticky() && isToolEvent(sig.Event) {
		return cur, false
	}
	return sig.State, true
}
```

Note the ordering: `postOfCandidate` is computed before `track` deletes the id.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/activity -v`
Expected: PASS for all five tests.

- [ ] **Step 5: Commit**

```bash
git add internal/activity
git commit -m "feat(activity): transition machine with tool-flight precedence"
```

### Task 7: Claude Code hook mapper and settings builder

**Files:**
- Create: `internal/activity/claude.go`
- Test: `internal/activity/claude_test.go`

**Interfaces:**
- Produces:

```go
// MapClaudeHook converts a raw Claude Code hook payload into a Signal.
// Returns ok=false for events that carry no activity meaning.
func MapClaudeHook(payload []byte) (sig Signal, ok bool, err error)

// ClaudeSettingsJSON returns the inline JSON for `claude --settings` that
// registers http hooks at hookURL for the given events.
func ClaudeSettingsJSON(hookURL string) string
```

- [ ] **Step 1: Write the failing test**

```go
package activity

import (
	"encoding/json"
	"testing"
)

func TestMapClaudeHook(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    Signal
		ok      bool
	}{
		{"session start", `{"hook_event_name":"SessionStart","session_id":"abc"}`, Signal{Event: "session-start", State: Active}, true},
		{"prompt submit", `{"hook_event_name":"UserPromptSubmit"}`, Signal{Event: "user-prompt-submit", State: Active}, true},
		{"pre tool", `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_use_id":"t1"}`, Signal{Event: "pre-tool-use", State: Active, ToolName: "Bash", ToolUseID: "t1"}, true},
		{"post tool", `{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_use_id":"t1"}`, Signal{Event: "post-tool-use", State: Active, ToolName: "Bash", ToolUseID: "t1"}, true},
		{"permission request", `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_use_id":"t1"}`, Signal{Event: "permission-request", State: Blocked, ToolName: "Bash", ToolUseID: "t1"}, true},
		{"notification permission", `{"hook_event_name":"Notification","notification_type":"permission_prompt"}`, Signal{Event: "notification", State: Blocked}, true},
		{"notification needs input", `{"hook_event_name":"Notification","notification_type":"agent_needs_input"}`, Signal{Event: "notification", State: WaitingInput}, true},
		{"notification idle", `{"hook_event_name":"Notification","notification_type":"idle_prompt"}`, Signal{Event: "notification", State: Idle}, true},
		{"notification auth ignored", `{"hook_event_name":"Notification","notification_type":"auth_success"}`, Signal{}, false},
		{"stop", `{"hook_event_name":"Stop"}`, Signal{Event: "stop", State: Idle}, true},
		{"session end exits", `{"hook_event_name":"SessionEnd","reason":"logout"}`, Signal{Event: "session-end", State: Exited}, true},
		{"session end clear ignored", `{"hook_event_name":"SessionEnd","reason":"clear"}`, Signal{}, false},
		{"unknown ignored", `{"hook_event_name":"PreCompact"}`, Signal{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok, err := MapClaudeHook([]byte(c.payload))
			if err != nil {
				t.Fatal(err)
			}
			if ok != c.ok || got != c.want {
				t.Fatalf("got %+v,%v want %+v,%v", got, ok, c.want, c.ok)
			}
		})
	}
}

func TestMapClaudeHookCarriesLaunchID(t *testing.T) {
	got, _, _ := MapClaudeHook([]byte(`{"hook_event_name":"Stop","agentboard_launch_id":"L1"}`))
	if got.LaunchID != "L1" {
		t.Fatalf("LaunchID = %q", got.LaunchID)
	}
}

func TestMapClaudeHookRejectsBadJSON(t *testing.T) {
	if _, _, err := MapClaudeHook([]byte(`{`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestClaudeSettingsJSON(t *testing.T) {
	raw := ClaudeSettingsJSON("http://127.0.0.1:1234/hooks/SES-01")
	var doc struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string            `json:"type"`
				URL     string            `json:"url"`
				Headers map[string]string `json:"headers"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, raw)
	}
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PermissionRequest", "Notification", "Stop", "SessionEnd"} {
		entries, ok := doc.Hooks[ev]
		if !ok || len(entries) == 0 || len(entries[0].Hooks) == 0 {
			t.Fatalf("missing hook for %s", ev)
		}
		h := entries[0].Hooks[0]
		if h.Type != "http" || h.URL != "http://127.0.0.1:1234/hooks/SES-01" {
			t.Fatalf("%s hook = %+v", ev, h)
		}
		if h.Headers["X-Agentboard-Launch"] != "$AGENTBOARD_LAUNCH_ID" {
			t.Fatalf("%s missing launch header", ev)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/activity -run 'Claude' -v`
Expected: compile errors.

- [ ] **Step 3: Write minimal implementation**

```go
package activity

import (
	"encoding/json"
	"fmt"
)

type claudeHook struct {
	Event            string `json:"hook_event_name"`
	ToolName         string `json:"tool_name"`
	ToolUseID        string `json:"tool_use_id"`
	NotificationType string `json:"notification_type"`
	Reason           string `json:"reason"`
	LaunchID         string `json:"agentboard_launch_id"`
}

func MapClaudeHook(payload []byte) (Signal, bool, error) {
	var h claudeHook
	if err := json.Unmarshal(payload, &h); err != nil {
		return Signal{}, false, fmt.Errorf("activity.mapClaudeHook: %w", err)
	}
	sig := Signal{ToolName: h.ToolName, ToolUseID: h.ToolUseID, LaunchID: h.LaunchID}
	switch h.Event {
	case "SessionStart":
		sig.Event, sig.State = "session-start", Active
	case "UserPromptSubmit":
		sig.Event, sig.State = "user-prompt-submit", Active
	case "PreToolUse":
		sig.Event, sig.State = "pre-tool-use", Active
	case "PostToolUse":
		sig.Event, sig.State = "post-tool-use", Active
	case "PermissionRequest":
		sig.Event, sig.State = "permission-request", Blocked
	case "Notification":
		sig.Event = "notification"
		switch h.NotificationType {
		case "permission_prompt":
			sig.State = Blocked
		case "agent_needs_input":
			sig.State = WaitingInput
		case "idle_prompt", "agent_completed":
			sig.State = Idle
		default:
			return Signal{}, false, nil
		}
	case "Stop":
		sig.Event, sig.State = "stop", Idle
	case "SessionEnd":
		if h.Reason == "clear" || h.Reason == "resume" {
			return Signal{}, false, nil
		}
		sig.Event, sig.State = "session-end", Exited
	default:
		return Signal{}, false, nil
	}
	return sig, true, nil
}

var claudeHookEvents = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PermissionRequest", "Notification", "Stop", "SessionEnd"}

func ClaudeSettingsJSON(hookURL string) string {
	type hook struct {
		Type           string            `json:"type"`
		URL            string            `json:"url"`
		Headers        map[string]string `json:"headers"`
		AllowedEnvVars []string          `json:"allowedEnvVars"`
		Timeout        int               `json:"timeout"`
	}
	type entry struct {
		Hooks []hook `json:"hooks"`
	}
	hooks := map[string][]entry{}
	for _, ev := range claudeHookEvents {
		timeout := 10
		if ev == "PermissionRequest" {
			timeout = 600
		}
		hooks[ev] = []entry{{Hooks: []hook{{
			Type:           "http",
			URL:            hookURL,
			Headers:        map[string]string{"X-Agentboard-Launch": "$AGENTBOARD_LAUNCH_ID"},
			AllowedEnvVars: []string{"AGENTBOARD_LAUNCH_ID"},
			Timeout:        timeout,
		}}}}
	}
	raw, _ := json.Marshal(map[string]any{"hooks": hooks})
	return string(raw)
}
```

The launch id arrives in a header because the hook body is Claude's own payload. The receiver in Task 11 copies the header into the signal.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/activity -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/activity
git commit -m "feat(activity): claude code hook mapper and settings builder"
```

### Task 8: Event hub

**Files:**
- Create: `internal/activity/hub.go`
- Test: `internal/activity/hub_test.go`

**Interfaces:**
- Produces:

```go
type Event struct {
	SessionID string
	TicketID  string
	Prev      State
	Next      State
	Request   *Request // non-nil when a pending request opened
}

type Hub struct{ ... }
func NewHub() *Hub
func (h *Hub) Subscribe() (ch <-chan Event, cancel func())
func (h *Hub) Publish(e Event)  // non-blocking; drops on a full subscriber
```

`Request` is defined in Task 10; for this task define it in `hub.go` as:

```go
type Request struct {
	ID        string
	SessionID string
	TicketID  string
	Kind      string // "permission" | "question"
	ToolName  string
	ToolInput string
}
```

- [ ] **Step 1: Write the failing test**

```go
package activity

import (
	"testing"
	"time"
)

func TestHubFanOutAndCancel(t *testing.T) {
	h := NewHub()
	a, cancelA := h.Subscribe()
	b, cancelB := h.Subscribe()
	defer cancelB()

	h.Publish(Event{SessionID: "S1", Prev: Active, Next: Blocked})
	for name, ch := range map[string]<-chan Event{"a": a, "b": b} {
		select {
		case e := <-ch:
			if e.SessionID != "S1" {
				t.Fatalf("%s got %+v", name, e)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s did not receive", name)
		}
	}

	cancelA()
	h.Publish(Event{SessionID: "S2"})
	select {
	case _, open := <-a:
		if open {
			t.Fatal("cancelled subscriber received")
		}
	case <-time.After(100 * time.Millisecond):
	}
	select {
	case e := <-b:
		if e.SessionID != "S2" {
			t.Fatal("b missed S2")
		}
	case <-time.After(time.Second):
		t.Fatal("b did not receive S2")
	}
}

func TestHubPublishNeverBlocks(t *testing.T) {
	h := NewHub()
	_, cancel := h.Subscribe()
	defer cancel()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			h.Publish(Event{SessionID: "S"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/activity -run Hub -v`
Expected: compile errors.

- [ ] **Step 3: Write minimal implementation**

```go
package activity

import "sync"

type Request struct {
	ID        string
	SessionID string
	TicketID  string
	Kind      string
	ToolName  string
	ToolInput string
}

type Event struct {
	SessionID string
	TicketID  string
	Prev      State
	Next      State
	Request   *Request
}

const subscriberBuffer = 64

type Hub struct {
	mu   sync.Mutex
	subs map[int]chan Event
	next int
}

func NewHub() *Hub { return &Hub{subs: map[int]chan Event{}} }

func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)
	h.mu.Lock()
	id := h.next
	h.next++
	h.subs[id] = ch
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if c, ok := h.subs[id]; ok {
			delete(h.subs, id)
			close(c)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) Publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs {
		select {
		case ch <- e:
		default:
		}
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/activity -race -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/activity
git commit -m "feat(activity): in-process event hub"
```

### Task 9: Store migration and session activity persistence

**Files:**
- Modify: `internal/store/migrations.go` (append after the last `ALTER` block, before `return nil`)
- Modify: `internal/store/sessions.go`
- Create: `internal/store/requests.go`
- Test: `internal/store/activity_test.go`

**Interfaces:**
- Produces:

```go
type SessionActivity struct {
	SessionID  string
	TicketID   string
	Agent      string
	State      string
	ActivityAt time.Time
	LaunchID   string
	StartedAt  time.Time
}
func (s *Store) SetActivity(ctx, sessionID, state string, at time.Time) error
func (s *Store) SetLaunchID(ctx, sessionID, launchID string) error
func (s *Store) ListActivity(ctx) ([]SessionActivity, error) // only sessions with ended_at IS NULL

type PendingRequest struct {
	ID, SessionID, TicketID, Kind, ToolName, ToolInput string
	CreatedAt time.Time
	ResolvedAt *time.Time
	Decision string
}
func (s *Store) CreatePendingRequest(ctx, r PendingRequest) (PendingRequest, error) // assigns REQ-NN id
func (s *Store) ResolvePendingRequest(ctx, id, decision string) error
func (s *Store) ListPendingRequests(ctx) ([]PendingRequest, error) // unresolved only
```

- [ ] **Step 1: Write the failing test**

```go
package store

import (
	"context"
	"testing"
	"time"
)

func TestSessionActivityRoundTrip(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()
	ctx := context.Background()
	sess, err := s.CreateSession(ctx, Session{TicketID: "AGT-01", Agent: "claude-code", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLaunchID(ctx, sess.ID, "L1"); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	if err := s.SetActivity(ctx, sess.ID, "blocked", at); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListActivity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].State != "blocked" || list[0].LaunchID != "L1" || !list[0].ActivityAt.Equal(at) || list[0].TicketID != "AGT-01" {
		t.Fatalf("ListActivity = %+v", list)
	}
	if err := s.EndSession(ctx, sess.ID, "completed"); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListActivity(ctx)
	if len(list) != 0 {
		t.Fatalf("ended session still listed: %+v", list)
	}
}

func TestPendingRequestLifecycle(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()
	ctx := context.Background()
	r, err := s.CreatePendingRequest(ctx, PendingRequest{SessionID: "SES-01", TicketID: "AGT-01", Kind: "permission", ToolName: "Bash", ToolInput: `{"command":"rm -rf build"}`})
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "REQ-01" {
		t.Fatalf("ID = %q", r.ID)
	}
	open, _ := s.ListPendingRequests(ctx)
	if len(open) != 1 || open[0].ToolName != "Bash" {
		t.Fatalf("open = %+v", open)
	}
	if err := s.ResolvePendingRequest(ctx, r.ID, "allow"); err != nil {
		t.Fatal(err)
	}
	open, _ = s.ListPendingRequests(ctx)
	if len(open) != 0 {
		t.Fatalf("resolved request still open: %+v", open)
	}
	if err := s.ResolvePendingRequest(ctx, "REQ-99", "allow"); err == nil {
		t.Fatal("expected not found")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store -run 'SessionActivity|PendingRequest' -v`
Expected: compile errors.

- [ ] **Step 3: Write minimal implementation**

In `migrations.go`, before the final `return nil`, add the same pragma-guarded pattern used for the other columns:

```go
	for _, col := range []struct{ name, ddl string }{
		{"activity_state", "ALTER TABLE sessions ADD COLUMN activity_state TEXT DEFAULT ''"},
		{"activity_at", "ALTER TABLE sessions ADD COLUMN activity_at DATETIME"},
		{"launch_id", "ALTER TABLE sessions ADD COLUMN launch_id TEXT DEFAULT ''"},
	} {
		err = s.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name=?", col.name).Scan(&hasCol)
		if err == nil && !hasCol {
			if _, err = s.db.Exec(col.ddl); err != nil {
				return err
			}
		}
	}

	_, err = s.db.Exec(`CREATE TABLE IF NOT EXISTS pending_requests (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		ticket_id TEXT DEFAULT '',
		kind TEXT NOT NULL,
		tool_name TEXT DEFAULT '',
		tool_input TEXT DEFAULT '',
		created_at DATETIME NOT NULL,
		resolved_at DATETIME,
		decision TEXT DEFAULT ''
	)`)
	if err != nil {
		return err
	}
```

`sessions.go` additions:

```go
type SessionActivity struct {
	SessionID  string
	TicketID   string
	Agent      string
	State      string
	ActivityAt time.Time
	LaunchID   string
	StartedAt  time.Time
}

func (s *Store) SetActivity(ctx context.Context, sessionID, state string, at time.Time) error {
	res, err := s.db.ExecContext(ctx, "UPDATE sessions SET activity_state = ?, activity_at = ? WHERE id = ?", state, at, sessionID)
	if err != nil {
		return fmt.Errorf("store.setActivity: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store.setActivity %s: %w", sessionID, ErrNotFound)
	}
	return nil
}

func (s *Store) SetLaunchID(ctx context.Context, sessionID, launchID string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE sessions SET launch_id = ? WHERE id = ?", launchID, sessionID)
	if err != nil {
		return fmt.Errorf("store.setLaunchID: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store.setLaunchID %s: %w", sessionID, ErrNotFound)
	}
	return nil
}

func (s *Store) ListActivity(ctx context.Context) ([]SessionActivity, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, ticket_id, agent, activity_state, activity_at, launch_id, started_at FROM sessions WHERE ended_at IS NULL ORDER BY started_at ASC")
	if err != nil {
		return nil, fmt.Errorf("store.listActivity: %w", err)
	}
	defer rows.Close()
	var out []SessionActivity
	for rows.Next() {
		var a SessionActivity
		var at sql.NullTime
		if err := rows.Scan(&a.SessionID, &a.TicketID, &a.Agent, &a.State, &at, &a.LaunchID, &a.StartedAt); err != nil {
			return nil, fmt.Errorf("store.listActivity: %w", err)
		}
		if at.Valid {
			a.ActivityAt = at.Time
		}
		out = append(out, a)
	}
	return out, nil
}
```

`requests.go`:

```go
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type PendingRequest struct {
	ID         string
	SessionID  string
	TicketID   string
	Kind       string
	ToolName   string
	ToolInput  string
	CreatedAt  time.Time
	ResolvedAt *time.Time
	Decision   string
}

const requestPrefix = "REQ-"

func (s *Store) CreatePendingRequest(ctx context.Context, r PendingRequest) (PendingRequest, error) {
	s.db.ExecContext(ctx, "INSERT OR IGNORE INTO id_counters (prefix, next_id) VALUES (?, 1)", requestPrefix)
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT next_id FROM id_counters WHERE prefix = ?", requestPrefix).Scan(&n); err != nil {
		return PendingRequest{}, fmt.Errorf("store.createPendingRequest: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE id_counters SET next_id = next_id + 1 WHERE prefix = ?", requestPrefix); err != nil {
		return PendingRequest{}, fmt.Errorf("store.createPendingRequest: %w", err)
	}
	r.ID = fmt.Sprintf("%s%02d", requestPrefix, n)
	r.CreatedAt = time.Now()
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO pending_requests (id, session_id, ticket_id, kind, tool_name, tool_input, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		r.ID, r.SessionID, r.TicketID, r.Kind, r.ToolName, r.ToolInput, r.CreatedAt)
	if err != nil {
		return PendingRequest{}, fmt.Errorf("store.createPendingRequest: %w", err)
	}
	return r, nil
}

func (s *Store) ResolvePendingRequest(ctx context.Context, id, decision string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE pending_requests SET resolved_at = ?, decision = ? WHERE id = ? AND resolved_at IS NULL", time.Now(), decision, id)
	if err != nil {
		return fmt.Errorf("store.resolvePendingRequest: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store.resolvePendingRequest %s: %w", id, ErrNotFound)
	}
	return nil
}

func (s *Store) ListPendingRequests(ctx context.Context) ([]PendingRequest, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, session_id, ticket_id, kind, tool_name, tool_input, created_at FROM pending_requests WHERE resolved_at IS NULL ORDER BY created_at ASC")
	if err != nil {
		return nil, fmt.Errorf("store.listPendingRequests: %w", err)
	}
	defer rows.Close()
	var out []PendingRequest
	for rows.Next() {
		var r PendingRequest
		if err := rows.Scan(&r.ID, &r.SessionID, &r.TicketID, &r.Kind, &r.ToolName, &r.ToolInput, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("store.listPendingRequests: %w", err)
		}
		out = append(out, r)
	}
	return out, nil
}

var _ = sql.ErrNoRows
```

Remove the trailing `var _ = sql.ErrNoRows` line if `database/sql` ends up unused; it is only there to keep the import valid while iterating.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/store -v 2>&1 | tail -10`
Expected: PASS including the existing migration idempotency tests.

- [ ] **Step 5: Commit**

```bash
git add internal/store
git commit -m "feat(store): session activity columns and pending_requests table"
```

### Task 10: Activity service

**Files:**
- Create: `internal/activity/service.go`
- Test: `internal/activity/service_test.go`

**Interfaces:**
- Consumes: `Apply`, `ToolFlight`, `Hub`, and a consumer-side store interface.
- Produces:

```go
type Store interface {
	SetActivity(ctx context.Context, sessionID, state string, at time.Time) error
	ListActivity(ctx context.Context) ([]store.SessionActivity, error)
	CreatePendingRequest(ctx context.Context, r store.PendingRequest) (store.PendingRequest, error)
	ResolvePendingRequest(ctx context.Context, id, decision string) error
	ListPendingRequests(ctx context.Context) ([]store.PendingRequest, error)
}

type Decision string
const (
	Allow Decision = "allow"
	Deny  Decision = "deny"
)

type Service struct{ ... }
func NewService(st Store, hub *Hub) *Service

// Register tells the service about a new session and its launch id.
func (s *Service) Register(sessionID, ticketID, launchID string)
// Forget drops in-memory state for an ended session and denies any open request.
func (s *Service) Forget(sessionID string)
// Apply applies a signal. Signals whose LaunchID mismatches are dropped.
func (s *Service) Apply(ctx context.Context, sessionID string, sig Signal) error
// OpenRequest records a pending request and returns a channel that receives the decision.
func (s *Service) OpenRequest(ctx context.Context, sessionID, kind, toolName, toolInput string) (store.PendingRequest, <-chan Decision, error)
// Resolve answers a request. Returns ErrNoSuchRequest if unknown.
func (s *Service) Resolve(ctx context.Context, requestID string, d Decision) error
// Snapshot returns the current state per session id (in-memory, authoritative).
func (s *Service) Snapshot() map[string]Info
// LastSignalAt for the reconciler.

type Info struct {
	SessionID string
	TicketID  string
	State     State
	Since     time.Time
	EverSignalled bool
}
```

The `Store` interface here lives in the activity package and uses `store.SessionActivity` and `store.PendingRequest` types, so the activity package imports `internal/store` (types only). That is allowed: `store` imports nothing from `activity`.

- [ ] **Step 1: Write the failing test**

```go
package activity

import (
	"context"
	"testing"
	"time"

	"github.com/ayandexyz/agent-board/internal/store"
)

type memStore struct {
	states   map[string]string
	requests []store.PendingRequest
	resolved map[string]string
}

func newMemStore() *memStore {
	return &memStore{states: map[string]string{}, resolved: map[string]string{}}
}
func (m *memStore) SetActivity(_ context.Context, id, state string, _ time.Time) error {
	m.states[id] = state
	return nil
}
func (m *memStore) ListActivity(context.Context) ([]store.SessionActivity, error) { return nil, nil }
func (m *memStore) CreatePendingRequest(_ context.Context, r store.PendingRequest) (store.PendingRequest, error) {
	r.ID = "REQ-01"
	m.requests = append(m.requests, r)
	return r, nil
}
func (m *memStore) ResolvePendingRequest(_ context.Context, id, d string) error {
	m.resolved[id] = d
	return nil
}
func (m *memStore) ListPendingRequests(context.Context) ([]store.PendingRequest, error) {
	return m.requests, nil
}

func TestServiceAppliesAndPublishes(t *testing.T) {
	st := newMemStore()
	hub := NewHub()
	ch, cancel := hub.Subscribe()
	defer cancel()
	svc := NewService(st, hub)
	svc.Register("S1", "AGT-01", "L1")

	if err := svc.Apply(context.Background(), "S1", Signal{Event: "permission-request", State: Blocked, ToolName: "Bash", LaunchID: "L1"}); err != nil {
		t.Fatal(err)
	}
	if st.states["S1"] != "blocked" {
		t.Fatalf("store state = %q", st.states["S1"])
	}
	select {
	case e := <-ch:
		if e.SessionID != "S1" || e.TicketID != "AGT-01" || e.Prev != Active || e.Next != Blocked {
			t.Fatalf("event = %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
	if got := svc.Snapshot()["S1"].State; got != Blocked {
		t.Fatalf("snapshot = %s", got)
	}
}

func TestServiceDropsMismatchedLaunch(t *testing.T) {
	st := newMemStore()
	svc := NewService(st, NewHub())
	svc.Register("S1", "AGT-01", "L1")
	_ = svc.Apply(context.Background(), "S1", Signal{Event: "stop", State: Idle, LaunchID: "STALE"})
	if _, ok := st.states["S1"]; ok {
		t.Fatal("stale launch signal was written")
	}
	_ = svc.Apply(context.Background(), "S1", Signal{Event: "stop", State: Idle})
	if st.states["S1"] != "idle" {
		t.Fatal("signal without launch id should be accepted")
	}
}

func TestServiceSuppressedSignalDoesNotPublish(t *testing.T) {
	hub := NewHub()
	ch, cancel := hub.Subscribe()
	defer cancel()
	svc := NewService(newMemStore(), hub)
	svc.Register("S1", "AGT-01", "")
	_ = svc.Apply(context.Background(), "S1", Signal{Event: "pre-tool-use", ToolName: "Read", ToolUseID: "t1"})
	select {
	case e := <-ch:
		t.Fatalf("unexpected event %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestServiceOpenAndResolveRequest(t *testing.T) {
	st := newMemStore()
	hub := NewHub()
	ch, cancel := hub.Subscribe()
	defer cancel()
	svc := NewService(st, hub)
	svc.Register("S1", "AGT-01", "")

	req, decision, err := svc.OpenRequest(context.Background(), "S1", "permission", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	e := <-ch
	if e.Request == nil || e.Request.ID != req.ID || e.Request.ToolName != "Bash" {
		t.Fatalf("event = %+v", e)
	}
	go func() { _ = svc.Resolve(context.Background(), req.ID, Allow) }()
	select {
	case d := <-decision:
		if d != Allow {
			t.Fatalf("decision = %s", d)
		}
	case <-time.After(time.Second):
		t.Fatal("no decision")
	}
	if st.resolved[req.ID] != "allow" {
		t.Fatal("store not updated")
	}
	if err := svc.Resolve(context.Background(), req.ID, Deny); err != ErrNoSuchRequest {
		t.Fatalf("second resolve err = %v", err)
	}
}

func TestServiceForgetDeniesOpenRequests(t *testing.T) {
	svc := NewService(newMemStore(), NewHub())
	svc.Register("S1", "", "")
	_, decision, _ := svc.OpenRequest(context.Background(), "S1", "permission", "Bash", "")
	svc.Forget("S1")
	select {
	case d := <-decision:
		if d != Deny {
			t.Fatalf("decision = %s", d)
		}
	case <-time.After(time.Second):
		t.Fatal("Forget did not resolve the open request")
	}
	if _, ok := svc.Snapshot()["S1"]; ok {
		t.Fatal("session still in snapshot")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/activity -run Service -v`
Expected: compile errors.

- [ ] **Step 3: Write minimal implementation**

```go
package activity

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ayandexyz/agent-board/internal/store"
)

var ErrNoSuchRequest = errors.New("activity: no such request")

type Store interface {
	SetActivity(ctx context.Context, sessionID, state string, at time.Time) error
	ListActivity(ctx context.Context) ([]store.SessionActivity, error)
	CreatePendingRequest(ctx context.Context, r store.PendingRequest) (store.PendingRequest, error)
	ResolvePendingRequest(ctx context.Context, id, decision string) error
	ListPendingRequests(ctx context.Context) ([]store.PendingRequest, error)
}

type Decision string

const (
	Allow Decision = "allow"
	Deny  Decision = "deny"
)

type Info struct {
	SessionID     string
	TicketID      string
	State         State
	Since         time.Time
	EverSignalled bool
}

type session struct {
	info     Info
	launchID string
	flight   ToolFlight
	requests map[string]chan Decision
}

type Service struct {
	store Store
	hub   *Hub
	now   func() time.Time

	mu       sync.Mutex
	sessions map[string]*session
	requests map[string]string // requestID -> sessionID
}

func NewService(st Store, hub *Hub) *Service {
	return &Service{store: st, hub: hub, now: time.Now, sessions: map[string]*session{}, requests: map[string]string{}}
}

func (s *Service) Register(sessionID, ticketID, launchID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = &session{
		info:     Info{SessionID: sessionID, TicketID: ticketID, State: Active, Since: s.now()},
		launchID: launchID,
		requests: map[string]chan Decision{},
	}
}

func (s *Service) Forget(sessionID string) {
	s.mu.Lock()
	sess, ok := s.sessions[sessionID]
	if ok {
		delete(s.sessions, sessionID)
		for id, ch := range sess.requests {
			delete(s.requests, id)
			ch <- Deny
		}
	}
	s.mu.Unlock()
}

func (s *Service) Apply(ctx context.Context, sessionID string, sig Signal) error {
	s.mu.Lock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	if sig.LaunchID != "" && sess.launchID != "" && sig.LaunchID != sess.launchID {
		s.mu.Unlock()
		return nil
	}
	prev := sess.info.State
	next, accepted := Apply(prev, &sess.flight, sig)
	sess.info.EverSignalled = true
	if !accepted {
		s.mu.Unlock()
		return nil
	}
	sess.info.State = next
	sess.info.Since = s.now()
	ticketID := sess.info.TicketID
	s.mu.Unlock()

	if err := s.store.SetActivity(ctx, sessionID, string(next), s.now()); err != nil {
		return err
	}
	if prev != next {
		s.hub.Publish(Event{SessionID: sessionID, TicketID: ticketID, Prev: prev, Next: next})
	}
	return nil
}

func (s *Service) OpenRequest(ctx context.Context, sessionID, kind, toolName, toolInput string) (store.PendingRequest, <-chan Decision, error) {
	s.mu.Lock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		s.mu.Unlock()
		return store.PendingRequest{}, nil, ErrNoSuchRequest
	}
	ticketID := sess.info.TicketID
	s.mu.Unlock()

	rec, err := s.store.CreatePendingRequest(ctx, store.PendingRequest{SessionID: sessionID, TicketID: ticketID, Kind: kind, ToolName: toolName, ToolInput: toolInput})
	if err != nil {
		return store.PendingRequest{}, nil, err
	}
	ch := make(chan Decision, 1)
	s.mu.Lock()
	sess.requests[rec.ID] = ch
	s.requests[rec.ID] = sessionID
	s.mu.Unlock()

	s.hub.Publish(Event{SessionID: sessionID, TicketID: ticketID, Prev: Blocked, Next: Blocked, Request: &Request{
		ID: rec.ID, SessionID: sessionID, TicketID: ticketID, Kind: kind, ToolName: toolName, ToolInput: toolInput,
	}})
	return rec, ch, nil
}

func (s *Service) Resolve(ctx context.Context, requestID string, d Decision) error {
	s.mu.Lock()
	sessionID, ok := s.requests[requestID]
	if !ok {
		s.mu.Unlock()
		return ErrNoSuchRequest
	}
	delete(s.requests, requestID)
	ch := s.sessions[sessionID].requests[requestID]
	delete(s.sessions[sessionID].requests, requestID)
	s.mu.Unlock()

	ch <- d
	return s.store.ResolvePendingRequest(ctx, requestID, string(d))
}

func (s *Service) Snapshot() map[string]Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]Info, len(s.sessions))
	for id, sess := range s.sessions {
		out[id] = sess.info
	}
	return out
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/activity -race -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/activity
git commit -m "feat(activity): service with launch guard, pending requests, snapshot"
```

### Task 11: Hook receiver

**Files:**
- Create: `internal/activity/receiver.go`
- Test: `internal/activity/receiver_test.go`

**Interfaces:**
- Produces:

```go
// Receiver serves POST /hooks/{sessionID}. PermissionRequest is held open
// until Resolve or timeout.
type Receiver struct{ ... }
func NewReceiver(svc *Service, permissionTimeout time.Duration) *Receiver
func (r *Receiver) Handler() http.Handler

// Serve binds 127.0.0.1:0 and returns the base URL, e.g. "http://127.0.0.1:41234".
func (r *Receiver) Serve(ctx context.Context) (baseURL string, err error)
```

- [ ] **Step 1: Write the failing test**

```go
package activity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestReceiver(t *testing.T) (*Service, *httptest.Server, *Hub) {
	t.Helper()
	hub := NewHub()
	svc := NewService(newMemStore(), hub)
	svc.Register("SES-01", "AGT-01", "L1")
	rec := NewReceiver(svc, 500*time.Millisecond)
	srv := httptest.NewServer(rec.Handler())
	t.Cleanup(srv.Close)
	return svc, srv, hub
}

func post(t *testing.T, url, launch, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if launch != "" {
		req.Header.Set("X-Agentboard-Launch", launch)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestReceiverAppliesStop(t *testing.T) {
	svc, srv, _ := newTestReceiver(t)
	resp := post(t, srv.URL+"/hooks/SES-01", "L1", `{"hook_event_name":"Stop"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got := svc.Snapshot()["SES-01"].State; got != Idle {
		t.Fatalf("state = %s", got)
	}
}

func TestReceiverIgnoresStaleLaunch(t *testing.T) {
	svc, srv, _ := newTestReceiver(t)
	post(t, srv.URL+"/hooks/SES-01", "OLD", `{"hook_event_name":"Stop"}`)
	if got := svc.Snapshot()["SES-01"].State; got != Active {
		t.Fatalf("stale hook applied: %s", got)
	}
}

func TestReceiverUnknownSessionIs404(t *testing.T) {
	_, srv, _ := newTestReceiver(t)
	resp := post(t, srv.URL+"/hooks/NOPE", "", `{"hook_event_name":"Stop"}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestReceiverPermissionRequestHeldUntilResolved(t *testing.T) {
	svc, srv, hub := newTestReceiver(t)
	events, cancel := hub.Subscribe()
	defer cancel()

	type result struct {
		body string
		code int
	}
	done := make(chan result, 1)
	go func() {
		resp := post(t, srv.URL+"/hooks/SES-01", "L1", `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_use_id":"t1","tool_input":{"command":"ls"}}`)
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			sb.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- result{sb.String(), resp.StatusCode}
	}()

	var reqID string
	select {
	case e := <-events:
		if e.Request == nil {
			e = <-events
		}
		if e.Request == nil || e.Request.ToolName != "Bash" || !strings.Contains(e.Request.ToolInput, "ls") {
			t.Fatalf("event = %+v", e)
		}
		reqID = e.Request.ID
	case <-time.After(time.Second):
		t.Fatal("no request event")
	}
	if got := svc.Snapshot()["SES-01"].State; got != Blocked {
		t.Fatalf("state = %s", got)
	}
	select {
	case <-done:
		t.Fatal("hook returned before resolve")
	case <-time.After(100 * time.Millisecond):
	}

	if err := svc.Resolve(context.Background(), reqID, Allow); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.code != http.StatusOK {
		t.Fatalf("status %d", r.code)
	}
	var out struct {
		HookSpecificOutput struct {
			HookEventName string `json:"hookEventName"`
			Decision      string `json:"decision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(r.body), &out); err != nil {
		t.Fatalf("bad body %q: %v", r.body, err)
	}
	if out.HookSpecificOutput.Decision != "allow" || out.HookSpecificOutput.HookEventName != "PermissionRequest" {
		t.Fatalf("body = %s", r.body)
	}
}

func TestReceiverPermissionRequestTimeoutReturnsEmpty(t *testing.T) {
	_, srv, _ := newTestReceiver(t)
	resp := post(t, srv.URL+"/hooks/SES-01", "L1", `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_use_id":"t1"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	if strings.TrimSpace(string(buf[:n])) != "{}" {
		t.Fatalf("body = %q, want {}", buf[:n])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/activity -run Receiver -v`
Expected: compile errors.

- [ ] **Step 3: Write minimal implementation**

```go
package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const maxHookBody = 1 << 20

type Receiver struct {
	svc               *Service
	permissionTimeout time.Duration
}

func NewReceiver(svc *Service, permissionTimeout time.Duration) *Receiver {
	return &Receiver{svc: svc, permissionTimeout: permissionTimeout}
}

func (r *Receiver) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /hooks/{sessionID}", r.handleHook)
	return mux
}

func (r *Receiver) Serve(ctx context.Context) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("activity.receiver: %w", err)
	}
	srv := &http.Server{Handler: r.Handler()}
	go func() { _ = srv.Serve(ln) }()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	return "http://" + ln.Addr().String(), nil
}

type rawHook struct {
	Event     string          `json:"hook_event_name"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

func (r *Receiver) handleHook(w http.ResponseWriter, req *http.Request) {
	sessionID := req.PathValue("sessionID")
	if _, ok := r.svc.Snapshot()[sessionID]; !ok {
		http.NotFound(w, req)
		return
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, maxHookBody))
	if err != nil {
		http.Error(w, "read", http.StatusBadRequest)
		return
	}
	sig, ok, err := MapClaudeHook(body)
	if err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}
	sig.LaunchID = req.Header.Get("X-Agentboard-Launch")
	w.Header().Set("Content-Type", "application/json")
	if !ok {
		_, _ = w.Write([]byte("{}"))
		return
	}
	_ = r.svc.Apply(req.Context(), sessionID, sig)

	if sig.Event != "permission-request" || r.svc.Snapshot()[sessionID].State != Blocked {
		_, _ = w.Write([]byte("{}"))
		return
	}

	var raw rawHook
	_ = json.Unmarshal(body, &raw)
	_, decision, err := r.svc.OpenRequest(req.Context(), sessionID, "permission", raw.ToolName, strings.TrimSpace(string(raw.ToolInput)))
	if err != nil {
		_, _ = w.Write([]byte("{}"))
		return
	}
	select {
	case d := <-decision:
		_ = json.NewEncoder(w).Encode(map[string]any{
			"hookSpecificOutput": map[string]string{"hookEventName": "PermissionRequest", "decision": string(d)},
		})
	case <-time.After(r.permissionTimeout):
		_, _ = w.Write([]byte("{}"))
	case <-req.Context().Done():
	}
}
```

The Snapshot guard before `OpenRequest` matters: if the launch id was stale, `Apply` dropped the signal and the state is not Blocked, so no request is opened.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/activity -race -v 2>&1 | tail -15`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/activity
git commit -m "feat(activity): loopback hook receiver with held permission requests"
```

### Task 12: Desktop notifier

**Files:**
- Create: `internal/activity/notify.go`
- Test: `internal/activity/notify_test.go`

**Interfaces:**
- Produces:

```go
type Sender func(title, body string) error
type Notifier struct{ ... }
// visible reports whether the ticket is currently on screen and the TUI focused.
func NewNotifier(hub *Hub, send Sender, visible func(ticketID string) bool) *Notifier
func (n *Notifier) Run(ctx context.Context)
func NotifySend() Sender // exec notify-send
```

- [ ] **Step 1: Write the failing test**

```go
package activity

import (
	"context"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) send(title, body string) error {
	r.mu.Lock()
	r.calls = append(r.calls, title+"|"+body)
	r.mu.Unlock()
	return nil
}
func (r *recorder) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.calls) }

func TestNotifierFiresOnEntryOnly(t *testing.T) {
	hub := NewHub()
	rec := &recorder{}
	n := NewNotifier(hub, rec.send, func(string) bool { return false })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.Run(ctx)
	time.Sleep(20 * time.Millisecond)

	hub.Publish(Event{SessionID: "S1", TicketID: "AGT-03", Prev: Active, Next: WaitingInput})
	hub.Publish(Event{SessionID: "S1", TicketID: "AGT-03", Prev: WaitingInput, Next: Blocked})
	hub.Publish(Event{SessionID: "S1", TicketID: "AGT-03", Prev: Blocked, Next: Active})
	hub.Publish(Event{SessionID: "S1", TicketID: "AGT-03", Prev: Active, Next: Blocked, Request: &Request{ToolName: "Bash", ToolInput: `{"command":"rm -rf build"}`}})
	time.Sleep(50 * time.Millisecond)

	if rec.count() != 2 {
		t.Fatalf("sent %d notifications, want 2: %v", rec.count(), rec.calls)
	}
	if rec.calls[1] != "AGT-03 needs approval|Bash {\"command\":\"rm -rf build\"}" {
		t.Fatalf("second = %q", rec.calls[1])
	}
}

func TestNotifierSkipsVisibleTicket(t *testing.T) {
	hub := NewHub()
	rec := &recorder{}
	n := NewNotifier(hub, rec.send, func(id string) bool { return id == "AGT-01" })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.Run(ctx)
	time.Sleep(20 * time.Millisecond)
	hub.Publish(Event{SessionID: "S1", TicketID: "AGT-01", Prev: Active, Next: Blocked})
	time.Sleep(50 * time.Millisecond)
	if rec.count() != 0 {
		t.Fatal("notified for a visible ticket")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/activity -run Notifier -v`
Expected: compile errors.

- [ ] **Step 3: Write minimal implementation**

```go
package activity

import (
	"context"
	"os/exec"
)

type Sender func(title, body string) error

type Notifier struct {
	hub     *Hub
	send    Sender
	visible func(ticketID string) bool
}

func NewNotifier(hub *Hub, send Sender, visible func(string) bool) *Notifier {
	return &Notifier{hub: hub, send: send, visible: visible}
}

func (n *Notifier) Run(ctx context.Context) {
	ch, cancel := n.hub.Subscribe()
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			if e.Prev.NeedsInput() || !e.Next.NeedsInput() {
				continue
			}
			if n.visible != nil && n.visible(e.TicketID) {
				continue
			}
			_ = n.send(title(e), body(e))
		}
	}
}

func title(e Event) string {
	name := e.TicketID
	if name == "" {
		name = e.SessionID
	}
	if e.Next == Blocked {
		return name + " needs approval"
	}
	return name + " is waiting for your answer"
}

func body(e Event) string {
	if e.Request == nil {
		return "session " + e.SessionID
	}
	if e.Request.ToolInput == "" {
		return e.Request.ToolName
	}
	return e.Request.ToolName + " " + e.Request.ToolInput
}

func NotifySend() Sender {
	return func(title, body string) error {
		return exec.Command("notify-send", "--app-name=agentboard", title, body).Run()
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/activity -race -v 2>&1 | tail -8`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/activity
git commit -m "feat(activity): desktop notifier on entry into needs-input"
```

### Task 13: Orchestrator passes env, settings, and registers with the activity service

**Files:**
- Modify: `internal/orchestrator/types.go` (`RunRequest.Env`, `RunRequest.ExtraArgs`)
- Modify: `internal/orchestrator/pty_runner.go` (window env, args after bin)
- Modify: `internal/orchestrator/service.go` (activity wiring)
- Test: `internal/orchestrator/pty_runner_test.go`, `internal/orchestrator/service_test.go`

**Interfaces:**
- Consumes: `activity.ClaudeSettingsJSON`, `activity.Service.Register/Forget`.
- Produces:

```go
type RunRequest struct {
	...
	Env       []string // KEY=VALUE, exported into the tmux window
	ExtraArgs []string // appended after cfg.Bin on the command line
}

// Activity is defined in orchestrator (consumer side).
type Activity interface {
	Register(sessionID, ticketID, launchID string)
	Forget(sessionID string)
}
func (s *Service) SetActivity(a Activity, hookBaseURL string)
```

- [ ] **Step 1: Write the failing tests**

`pty_runner_test.go`:

```go
func TestPtyRunnerStartExportsEnvAndExtraArgs(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-test,123,0")
	t.Setenv("HOME", t.TempDir())
	tmuxDir := t.TempDir()
	logFile := filepath.Join(tmuxDir, "tmux.log")
	t.Setenv("FAKE_TMUX_LOG", logFile)
	t.Setenv("PATH", tmuxDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	fakeTmux := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$FAKE_TMUX_LOG\"\ncase \"$1\" in new-window) printf '%%42\\n';; capture-pane) printf 'bypass permissions on\\n';; list-panes) exit 1;; esac\n"
	if err := os.WriteFile(filepath.Join(tmuxDir, "tmux"), []byte(fakeTmux), 0755); err != nil {
		t.Fatal(err)
	}
	runner, _ := NewTmuxAgentRunner("agentboard")
	handle, err := runner.Start(context.Background(), RunRequest{
		SessionID: "SES-05",
		Agent:     "claude-code",
		Prompt:    "x",
		Env:       []string{"AGENTBOARD_SESSION_ID=SES-05", "AGENTBOARD_LAUNCH_ID=L5"},
		ExtraArgs: []string{"--settings", `{"hooks":{}}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Stop()
	time.Sleep(100 * time.Millisecond)
	raw, _ := os.ReadFile(logFile)
	log := string(raw)
	if !strings.Contains(log, "-e AGENTBOARD_SESSION_ID=SES-05") || !strings.Contains(log, "-e AGENTBOARD_LAUNCH_ID=L5") {
		t.Fatalf("env not passed to new-window:\n%s", log)
	}
	if !strings.Contains(log, `send-keys -t %42 -l claude --settings '{"hooks":{}}'`) {
		t.Fatalf("extra args not on command line:\n%s", log)
	}
}
```

`service_test.go`:

```go
type fakeActivity struct {
	registered []string
	forgotten  []string
}

func (f *fakeActivity) Register(sessionID, ticketID, launchID string) {
	f.registered = append(f.registered, sessionID+"/"+ticketID+"/"+launchID)
}
func (f *fakeActivity) Forget(id string) { f.forgotten = append(f.forgotten, id) }

type capturingRunner struct {
	fakeAgentRunner
	last RunRequest
}

func (c *capturingRunner) Start(ctx context.Context, req RunRequest) (RunHandle, error) {
	c.last = req
	return c.fakeAgentRunner.Start(ctx, req)
}

func TestStartAdHocRunRegistersActivityAndInjectsHooks(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	fr := &capturingRunner{}
	fa := &fakeActivity{}
	svc := NewService(fs, nil, nil, nil)
	svc.SetAgentRunner(fr)
	svc.SetActivity(fa, "http://127.0.0.1:9/")

	sess, err := svc.StartAdHocRun(context.Background(), "AGT-01", "claude-code", "do it")
	if err != nil {
		t.Fatal(err)
	}
	if len(fa.registered) != 1 || !strings.HasPrefix(fa.registered[0], sess.ID+"/AGT-01/") {
		t.Fatalf("registered = %v", fa.registered)
	}
	launch := strings.TrimPrefix(fa.registered[0], sess.ID+"/AGT-01/")
	if launch == "" {
		t.Fatal("empty launch id")
	}
	env := strings.Join(fr.last.Env, "\n")
	for _, want := range []string{"AGENTBOARD_SESSION_ID=" + sess.ID, "AGENTBOARD_LAUNCH_ID=" + launch, "AGENTBOARD_HOOK_URL=http://127.0.0.1:9"} {
		if !strings.Contains(env, want) {
			t.Fatalf("env missing %q: %v", want, fr.last.Env)
		}
	}
	if len(fr.last.ExtraArgs) != 2 || fr.last.ExtraArgs[0] != "--settings" || !strings.Contains(fr.last.ExtraArgs[1], "http://127.0.0.1:9/hooks/"+sess.ID) {
		t.Fatalf("ExtraArgs = %v", fr.last.ExtraArgs)
	}
	if fs.lastSession.ID != sess.ID {
		t.Fatal("session not created")
	}

	if err := svc.StopSession(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if len(fa.forgotten) != 1 || fa.forgotten[0] != sess.ID {
		t.Fatalf("forgotten = %v", fa.forgotten)
	}
}

func TestStartAdHocRunNonClaudeGetsNoSettings(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	fr := &capturingRunner{}
	svc := NewService(fs, nil, nil, nil)
	svc.SetAgentRunner(fr)
	svc.SetActivity(&fakeActivity{}, "http://127.0.0.1:9")
	if _, err := svc.StartAdHocRun(context.Background(), "AGT-01", "opencode", "x"); err != nil {
		t.Fatal(err)
	}
	if len(fr.last.ExtraArgs) != 0 {
		t.Fatalf("opencode got ExtraArgs %v", fr.last.ExtraArgs)
	}
}
```

Add `"strings"` and the store import to `service_test.go` if missing. The `fakeStore` in `helpers_test.go` needs `SetLaunchID(ctx, id, launch string) error { return nil }` once the `Store` interface gains it below.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/orchestrator -run 'ExportsEnv|RegistersActivity|NonClaude' -v`
Expected: compile errors.

- [ ] **Step 3: Write minimal implementation**

`types.go`: add `Env []string` and `ExtraArgs []string` to `RunRequest`. Add to the `Store` interface: `SetLaunchID(ctx context.Context, sessionID, launchID string) error`. Add:

```go
type Activity interface {
	Register(sessionID, ticketID, launchID string)
	Forget(sessionID string)
}
```

`pty_runner.go`: `newWindowArgs(sessionName, windowName, workDir string, env []string)` appends `"-e", kv` for each env entry (tmux `new-window -e` sets environment for the new pane). Update `window_args_test.go` callers to pass `nil`. Build the launch command line:

```go
	cmdline := cfg.Bin
	for _, a := range req.ExtraArgs {
		cmdline += " " + shellQuote(a)
	}
	if err := exec.Command("tmux", "send-keys", "-t", paneID, "-l", cmdline).Run(); err != nil { ... }
	if err := exec.Command("tmux", "send-keys", "-t", paneID, "Enter").Run(); err != nil { ... }
```

with

```go
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`{}[]()<>|&;*?") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
```

Note the existing Start test asserts on a `send-keys -t %42 opencode Enter` line; update that assertion to the two-line form (`-l opencode` then `Enter`).

`service.go`: add fields `activity Activity` and `hookBaseURL string`, plus:

```go
func (s *Service) SetActivity(a Activity, hookBaseURL string) {
	s.activity = a
	s.hookBaseURL = strings.TrimRight(hookBaseURL, "/")
}

func (s *Service) launchEnv(sessionID, launchID string) ([]string, []string) {
	env := []string{
		"AGENTBOARD_SESSION_ID=" + sessionID,
		"AGENTBOARD_LAUNCH_ID=" + launchID,
	}
	if s.hookBaseURL == "" {
		return env, nil
	}
	env = append(env, "AGENTBOARD_HOOK_URL="+s.hookBaseURL)
	return env, nil
}

func isClaude(agent string) bool { return agent == "claude-code" || agent == "claudecode" }
```

In both `StartAdHocRun` and `StartApprovedRun`, after `CreateSession`:

```go
	launchID := uuid.NewString()
	_ = s.store.SetLaunchID(ctx, session.ID, launchID)
	env, _ := s.launchEnv(session.ID, launchID)
	var extra []string
	if isClaude(agent) && s.hookBaseURL != "" {
		extra = []string{"--settings", activity.ClaudeSettingsJSON(s.hookBaseURL + "/hooks/" + session.ID)}
	}
	if s.activity != nil {
		s.activity.Register(session.ID, ticketID, launchID)
	}
```

and pass `Env: env, ExtraArgs: extra` in the `RunRequest`. On every error path after registration, and inside `onComplete` and `StopSession`, call `s.activity.Forget(session.ID)` when `s.activity != nil`. `github.com/google/uuid` is already in go.mod.

The orchestrator imports `internal/activity` only for `ClaudeSettingsJSON`. `activity` does not import `orchestrator`, so there is no cycle.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/orchestrator -race 2>&1 | tail -10 && go vet ./...`
Expected: PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator
git commit -m "feat(orchestrator): pass session env and claude hook settings; register with activity"
```

### Task 14: Wire the receiver, notifier, and service in main

**Files:**
- Modify: `cmd/agentboard/run.go:68-96`
- Modify: `internal/tui/app.go` (`Orchestrator` interface gains `Activity() *activity.Service`)
- Test: `internal/orchestrator/service_test.go` (accessor), manual smoke

**Interfaces:**
- Produces: `Service.ActivityService() *activity.Service` on the orchestrator (nil when not wired). `run.go` starts the receiver on `127.0.0.1:0`, constructs the notifier with `activity.NotifySend()` and a `visible` func that the TUI sets later.

- [ ] **Step 1: Write the failing test**

```go
func TestActivityServiceAccessor(t *testing.T) {
	svc := NewService(&fakeStore{}, nil, nil, nil)
	if svc.ActivityService() != nil {
		t.Fatal("expected nil before wiring")
	}
	as := activity.NewService(nil, activity.NewHub())
	svc.SetActivity(as, "http://127.0.0.1:1")
	if svc.ActivityService() != as {
		t.Fatal("accessor did not return the wired service")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/orchestrator -run TestActivityServiceAccessor -v`
Expected: compile error.

- [ ] **Step 3: Write minimal implementation**

In `service.go`:

```go
func (s *Service) ActivityService() *activity.Service {
	if as, ok := s.activity.(*activity.Service); ok {
		return as
	}
	return nil
}
```

In `run.go` after `orch.SetAgentRunner(agentRunner)`:

```go
	hub := activity.NewHub()
	actSvc := activity.NewService(s, hub)
	receiver := activity.NewReceiver(actSvc, 10*time.Minute)
	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()
	if hookURL, err := receiver.Serve(appCtx); err == nil {
		orch.SetActivity(actSvc, hookURL)
	} else {
		fmt.Fprintf(os.Stderr, "activity receiver disabled: %v\n", err)
		orch.SetActivity(actSvc, "")
	}
	visible := tui.VisibleTicketFunc()
	go activity.NewNotifier(hub, activity.NotifySend(), visible.Check).Run(appCtx)
```

`tui.VisibleTicketFunc` is added in Task 16; for this task add a minimal version in `internal/tui/visibility.go`:

```go
package tui

import "sync"

type Visibility struct {
	mu      sync.Mutex
	focused bool
	tickets map[string]bool
}

func VisibleTicketFunc() *Visibility { return &Visibility{tickets: map[string]bool{}} }

func (v *Visibility) Check(ticketID string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.focused && v.tickets[ticketID]
}

func (v *Visibility) Set(focused bool, ids []string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.focused = focused
	v.tickets = make(map[string]bool, len(ids))
	for _, id := range ids {
		v.tickets[id] = true
	}
}
```

Pass `visible` into `tui.AppDeps` as `Visibility *Visibility` and store it on `App`. Add `Activity() *activity.Service` to the TUI `Orchestrator` interface and have `Service.Activity()` alias `ActivityService()` (name the orchestrator method `ActivityService` and the interface method the same to avoid two names).

- [ ] **Step 4: Run tests and build**

Run: `go build ./... && go test ./... 2>&1 | tail -15 && go vet ./...`
Expected: all PASS.

- [ ] **Step 5: Smoke test manually**

Run inside tmux: `AGENTBOARD_LOG=debug ./agentboard`, start a `claude-code` ticket, and in the pane run `/hooks` inside Claude Code to confirm the eight http hooks are listed with the receiver URL. Trigger a permission prompt (any Bash command) and confirm `notify-send` fires.

- [ ] **Step 6: Commit**

```bash
git add cmd/agentboard/run.go internal/orchestrator/service.go internal/orchestrator/service_test.go internal/tui/visibility.go internal/tui/app.go
git commit -m "feat(cmd): wire activity receiver and notifier at startup"
```

---

## Phase 3: TUI

### Task 15: Keybinding action for the waiting overlay

**Files:**
- Modify: `internal/keybinding/action.go`, `internal/keybinding/keymap.go`
- Test: `internal/keybinding/waiting_key_test.go`

- [ ] **Step 1: Write the failing test**

Model on `internal/keybinding/merge_key_test.go`:

```go
package keybinding

import "testing"

func TestWaitingKeyBound(t *testing.T) {
	km := DefaultKeyMap()
	b, ok := km.Lookup("w")
	if !ok || b.Action != ActionShowWaiting {
		t.Fatalf("w bound to %v, want ActionShowWaiting", b.Action)
	}
	if got := ActionShowWaiting.String(); got != "show_waiting" {
		t.Fatalf("String() = %q", got)
	}
}
```

Adjust `DefaultKeyMap` and `Lookup` names to whatever `merge_key_test.go` uses.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/keybinding -run TestWaitingKeyBound -v`
Expected: compile error.

- [ ] **Step 3: Write minimal implementation**

Add `ActionShowWaiting` to the action enum in `action.go` with the `"show_waiting"` string case, add it to the ordered action list in `keymap.go:41` region, and add `{Key: "w", Action: ActionShowWaiting, Description: "waiting agents"}` next to the merge binding.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/keybinding -v 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/keybinding
git commit -m "feat(keybinding): w opens the waiting agents overlay"
```

### Task 16: Activity glyph on ticket cards and waiting count in header

**Files:**
- Modify: `internal/tui/ticketcard.go:76-92,177-215`
- Modify: `internal/tui/kanban.go` (pass activity map into cards)
- Modify: `internal/tui/app.go` (subscription cmd, `activityChangedMsg`, header count, visibility updates)
- Test: `internal/tui/ticketcard_test.go`, `internal/tui/app_test.go`

**Interfaces:**
- Produces: `NewTicketCardModel(..., activityState string)` and `ActivityGlyph(state string) string`. `App.activity map[string]string` keyed by ticket id. `activityChangedMsg{}`.

- [ ] **Step 1: Write the failing tests**

`ticketcard_test.go`:

```go
func TestActivityGlyph(t *testing.T) {
	cases := map[string]string{
		"active": "●", "idle": "○", "waiting_input": "?", "blocked": "!", "exited": "×", "no_signal": "~", "": "",
	}
	for state, want := range cases {
		if got := ActivityGlyph(state); got != want {
			t.Errorf("ActivityGlyph(%q) = %q want %q", state, got, want)
		}
	}
}

func TestTicketCardRendersBlockedGlyph(t *testing.T) {
	tk := store.Ticket{ID: "AGT-03", Title: "x", Priority: "medium", Agent: "claude-code", AgentActive: true}
	card := NewTicketCardModel(tk, false, false, 40, 0, nil, "blocked")
	if !strings.Contains(card.Render(), "!") {
		t.Fatal("blocked glyph not rendered")
	}
}
```

`app_test.go` (follow the existing constructor helper in that file):

```go
func TestHeaderShowsWaitingCount(t *testing.T) {
	a := newTestApp(t)
	a.width = 120
	a.activity = map[string]string{"AGT-01": "blocked", "AGT-02": "waiting_input", "AGT-03": "active"}
	if !strings.Contains(a.renderHeader(), "2 waiting") {
		t.Fatalf("header = %q", a.renderHeader())
	}
	a.activity = map[string]string{"AGT-03": "active"}
	if strings.Contains(a.renderHeader(), "waiting") {
		t.Fatal("header shows waiting with none pending")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'ActivityGlyph|BlockedGlyph|WaitingCount' -v`
Expected: compile errors.

- [ ] **Step 3: Write minimal implementation**

`ticketcard.go`:

```go
func ActivityGlyph(state string) string {
	switch state {
	case "active":
		return "●"
	case "idle":
		return "○"
	case "waiting_input":
		return "?"
	case "blocked":
		return "!"
	case "exited":
		return "×"
	case "no_signal":
		return "~"
	}
	return ""
}
```

Add `activity string` to `TicketCardModel`, a trailing `activityState string` parameter to `NewTicketCardModel`, and in `renderFooter` prepend `ActivityGlyph(c.activity)` to `leftParts` when non-empty, styled with the priority color for `blocked` (`critical` color) and `waiting_input` (`high` color). Update every existing `NewTicketCardModel` call to pass `""` or the map lookup.

`kanban.go`: add `activity map[string]string` to `KanbanModel` with `func (m KanbanModel) WithActivity(a map[string]string) KanbanModel`, and pass `m.activity[t.ID]` when building each card.

`app.go`:

```go
type activityChangedMsg struct{}

func (a *App) activityCmd() tea.Cmd {
	as := a.orchestrator.ActivityService()
	if as == nil {
		return nil
	}
	if a.activityCh == nil {
		a.activityCh, a.activityCancel = as.Hub().Subscribe()
	}
	ch := a.activityCh
	return func() tea.Msg {
		if _, ok := <-ch; !ok {
			return nil
		}
		return activityChangedMsg{}
	}
}

func (a *App) refreshActivity() {
	as := a.orchestrator.ActivityService()
	if as == nil {
		return
	}
	a.activity = map[string]string{}
	for _, info := range as.Snapshot() {
		if info.TicketID == "" {
			continue
		}
		a.activity[info.TicketID] = string(info.State)
	}
	a.kanban = a.kanban.WithActivity(a.activity)
}
```

`activity.Service` needs `func (s *Service) Hub() *Hub { return s.hub }`; add it in `internal/activity/service.go`.

In `Update`, handle `activityChangedMsg`: call `refreshActivity()`, update visibility with the ids currently rendered (`a.visibility.Set(true, a.kanban.VisibleTicketIDs())`, adding that helper to kanban which returns the ids of cards currently drawn), and return `a.activityCmd()` to re-arm. Call `a.activityCmd()` from `Init` alongside existing cmds. Also call `refreshActivity()` inside the `runCompletedMsg` and `agentActiveChangedMsg` handlers.

`renderHeader`: count `a.activity` values equal to `blocked` or `waiting_input`; when non-zero insert `waitStyle.Render(fmt.Sprintf(" %d waiting ", n))` between `projectPart` and the right hints, with `waitStyle` bold on the theme's `Warning` color or `214` fallback. Include its width in the gap arithmetic.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui 2>&1 | tail -10`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui internal/activity/service.go
git commit -m "feat(tui): activity glyph per card and waiting count in header"
```

### Task 17: Waiting overlay with resolve and jump

**Files:**
- Create: `internal/tui/waiting_overlay.go`
- Modify: `internal/tui/app.go` (open on `w`, route keys, render like `mergeModal`)
- Modify: `internal/orchestrator/service.go` (`ResolveRequest`, `ListPendingRequests` pass-throughs)
- Test: `internal/tui/waiting_overlay_test.go`, `internal/tui/app_test.go`

**Interfaces:**
- Produces:

```go
type WaitingItem struct {
	RequestID string // empty when the session is waiting_input with no hook request
	SessionID string
	TicketID  string
	State     string
	ToolName  string
	ToolInput string
}

type WaitingOverlay struct{ ... }
func (o *WaitingOverlay) SetTheme(t *theme.Theme)
func (o *WaitingOverlay) SetSize(w, h int)
func (o *WaitingOverlay) Active() bool
func (o *WaitingOverlay) Open(items []WaitingItem)
func (o *WaitingOverlay) Update(msg tea.Msg) (WaitingOverlay, tea.Cmd)
func (o *WaitingOverlay) ViewBox() string

type waitingResolveMsg struct{ requestID string; decision string }
type waitingJumpMsg struct{ sessionID string }
type waitingClosedMsg struct{}
```

Orchestrator additions on `Service` and the TUI `Orchestrator` interface:

```go
func (s *Service) ListWaiting(ctx context.Context) ([]WaitingSession, error)
func (s *Service) ResolveRequest(ctx context.Context, requestID, decision string) error

type WaitingSession struct {
	SessionID, TicketID, State, RequestID, ToolName, ToolInput string
}
```

- [ ] **Step 1: Write the failing tests**

`waiting_overlay_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestWaitingOverlayKeys(t *testing.T) {
	var o WaitingOverlay
	o.SetSize(80, 24)
	o.Open([]WaitingItem{
		{RequestID: "REQ-01", SessionID: "SES-01", TicketID: "AGT-01", State: "blocked", ToolName: "Bash", ToolInput: `{"command":"ls"}`},
		{SessionID: "SES-02", TicketID: "AGT-02", State: "waiting_input"},
	})
	if !o.Active() {
		t.Fatal("not active after Open")
	}
	view := o.ViewBox()
	for _, want := range []string{"AGT-01", "Bash", "AGT-02", "waiting"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}

	o, cmd := o.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if msg, ok := cmd().(waitingResolveMsg); !ok || msg.requestID != "REQ-01" || msg.decision != "allow" {
		t.Fatalf("y produced %#v", cmd())
	}

	o, _ = o.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	o, cmd = o.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd != nil {
		t.Fatal("y on a session without a request should do nothing")
	}
	o, cmd = o.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msg, ok := cmd().(waitingJumpMsg); !ok || msg.sessionID != "SES-02" {
		t.Fatalf("enter produced %#v", cmd())
	}
	o, cmd = o.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if o.Active() {
		t.Fatal("esc did not close")
	}
	if _, ok := cmd().(waitingClosedMsg); !ok {
		t.Fatal("esc did not emit waitingClosedMsg")
	}
}
```

`app_test.go`:

```go
func TestAppResolvesFromOverlay(t *testing.T) {
	a := newTestApp(t)
	fo := a.orchestrator.(*fakeOrchestrator)
	_, _ = a.Update(waitingResolveMsg{requestID: "REQ-01", decision: "deny"})
	if fo.resolved != "REQ-01/deny" {
		t.Fatalf("resolved = %q", fo.resolved)
	}
}
```

Extend the existing `fakeOrchestrator` in `app_test.go` with `resolved string`, `ResolveRequest`, `ListWaiting`, and `ActivityService() *activity.Service { return nil }`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'WaitingOverlay|ResolvesFromOverlay' -v`
Expected: compile errors.

- [ ] **Step 3: Write minimal implementation**

`orchestrator/service.go`:

```go
type WaitingSession struct {
	SessionID, TicketID, State, RequestID, ToolName, ToolInput string
}

func (s *Service) ListWaiting(ctx context.Context) ([]WaitingSession, error) {
	as := s.ActivityService()
	if as == nil {
		return nil, nil
	}
	pending, err := s.store.ListPendingRequests(ctx)
	if err != nil {
		return nil, err
	}
	byTicket := map[string]store.PendingRequest{}
	for _, p := range pending {
		byTicket[p.SessionID] = p
	}
	var out []WaitingSession
	for id, info := range as.Snapshot() {
		if !info.State.NeedsInput() {
			continue
		}
		w := WaitingSession{SessionID: id, TicketID: info.TicketID, State: string(info.State)}
		if p, ok := byTicket[id]; ok {
			w.RequestID, w.ToolName, w.ToolInput = p.ID, p.ToolName, p.ToolInput
		}
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TicketID < out[j].TicketID })
	return out, nil
}

func (s *Service) ResolveRequest(ctx context.Context, requestID, decision string) error {
	as := s.ActivityService()
	if as == nil {
		return fmt.Errorf("orchestrator.resolveRequest: activity tracking disabled")
	}
	return as.Resolve(ctx, requestID, activity.Decision(decision))
}
```

Add `ListPendingRequests` to the orchestrator `Store` interface and the `fakeStore`.

`waiting_overlay.go`: follow `merge_modal.go` for styles and box rendering. Keys: `j`/`k`/arrows move cursor; `y` emits `waitingResolveMsg{RequestID, "allow"}` only when `RequestID != ""`; `n` the same with `"deny"`; `Enter` emits `waitingJumpMsg{SessionID}`; `Esc`/`q`/`w` closes and emits `waitingClosedMsg{}`. Each row renders `glyph TicketID  ToolName ToolInput(truncated to 40 cols)` or `glyph TicketID  waiting for your answer`. A footer line reads `y allow  n deny  enter jump  esc close`.

`app.go`: on `ActionShowWaiting`, call `a.orchestrator.ListWaiting`, map to `WaitingItem`, `a.waiting.Open(items)`. When `a.waiting.Active()`, route key msgs to it before the kanban, mirroring the `mergeModal` block at `app.go:659`. Handle `waitingResolveMsg` by calling `ResolveRequest` in a cmd and showing a notification on error, then reopen the overlay with fresh items; `waitingJumpMsg` calls `SwitchToPane(sessionID)` and closes; `waitingClosedMsg` is a no-op. Render `a.waiting.ViewBox()` as an overlay in `View` the same way `mergeModal` is rendered.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui ./internal/orchestrator 2>&1 | tail -10 && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui internal/orchestrator
git commit -m "feat(tui): waiting overlay with allow/deny and jump to pane"
```

### Task 18: Guarded SendInput

**Files:**
- Modify: `internal/orchestrator/service.go:413-418`
- Test: `internal/orchestrator/service_test.go`

**Interfaces:**
- Produces: `ErrInputSuppressed` with a reason; `SendInput` writes via `agentRunner` pane using chunked `send-keys -l`, 300 ms pause, then `Enter`.

- [ ] **Step 1: Write the failing test**

```go
func TestSendInputRefusesBlockedAndExited(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	fr := &capturingRunner{}
	hub := activity.NewHub()
	as := activity.NewService(newActivityMemStore(), hub)
	svc := NewService(fs, nil, nil, nil)
	svc.SetAgentRunner(fr)
	svc.SetActivity(as, "")
	sess, _ := svc.StartAdHocRun(context.Background(), "AGT-01", "opencode", "x")

	_ = as.Apply(context.Background(), sess.ID, activity.Signal{Event: "permission-request", State: activity.Blocked, ToolName: "Bash"})
	if err := svc.SendInput(sess.ID, "hello"); !errors.Is(err, ErrInputSuppressed) {
		t.Fatalf("blocked: err = %v", err)
	}
	_ = as.Apply(context.Background(), sess.ID, activity.Signal{Event: "session-end", State: activity.Exited})
	if err := svc.SendInput(sess.ID, "hello"); !errors.Is(err, ErrInputSuppressed) {
		t.Fatalf("exited: err = %v", err)
	}
}
```

`newActivityMemStore` is a copy of the `memStore` from Task 10 placed in `helpers_test.go` (test doubles cannot be shared across packages).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/orchestrator -run TestSendInputRefuses -v`
Expected: compile error on `ErrInputSuppressed`.

- [ ] **Step 3: Write minimal implementation**

```go
var ErrInputSuppressed = errors.New("orchestrator: input suppressed")

func (s *Service) SendInput(sessionID, input string) error {
	if as := s.ActivityService(); as != nil {
		if info, ok := as.Snapshot()[sessionID]; ok {
			switch info.State {
			case activity.Blocked:
				return fmt.Errorf("%w: session is blocked on a permission dialog", ErrInputSuppressed)
			case activity.Exited:
				return fmt.Errorf("%w: agent has exited, pane holds a shell", ErrInputSuppressed)
			}
		}
	}
	if s.agentRunner == nil {
		return fmt.Errorf("orchestrator.sendInput: no agent runner")
	}
	paneID, ok := s.agentRunner.GetPaneID(sessionID)
	if !ok {
		return fmt.Errorf("orchestrator.sendInput: no pane for %s", sessionID)
	}
	return sendKeysChunked(paneID, input)
}
```

In `pty_runner.go`:

```go
const sendChunk = 16 * 1024

func sendKeysChunked(paneID, text string) error {
	for i := 0; i < len(text); i += sendChunk {
		end := i + sendChunk
		if end > len(text) {
			end = len(text)
		}
		if err := exec.Command("tmux", "send-keys", "-t", paneID, "-l", text[i:end]).Run(); err != nil {
			return fmt.Errorf("orchestrator.sendKeys: %w", err)
		}
	}
	time.Sleep(300 * time.Millisecond)
	if err := exec.Command("tmux", "send-keys", "-t", paneID, "Enter").Run(); err != nil {
		return fmt.Errorf("orchestrator.sendKeys enter: %w", err)
	}
	return nil
}
```

Also replace the per-rune loop in `injectPrompt` with `sendKeysChunked(paneID, strings.ReplaceAll(prompt, "\n", " "))`, keeping the existing newline-to-space behaviour that `TestPtyRunnerStartSendsNewlinesAsSpaceNotEnterOrLiteralCtrlM` asserts. Update that test's expectations from per-character lines to a single `-l` line.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/orchestrator -race 2>&1 | tail -10`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator
git commit -m "feat(orchestrator): guard SendInput on activity state; chunked send-keys"
```

---

## Phase 4: Fallback reconciler

### Task 19: Structural screen classifier

**Files:**
- Create: `internal/activity/screen.go`
- Test: `internal/activity/screen_test.go`, fixtures in `internal/activity/testdata/*.txt`

**Interfaces:**
- Produces: `func ClassifyScreen(text string) State` returning `Active`, `Idle`, `WaitingInput`, or `""` when unsure. Never returns `Blocked`.

- [ ] **Step 1: Write the failing test and fixtures**

`testdata/claude_active.txt`:

```
╭──────────────────────────────────────────────╮
│ > fix the failing test                        │
╰──────────────────────────────────────────────╯
✻ Thinking… (esc to interrupt)
╭──────────────────────────────────────────────╮
│ ❯                                             │
╰──────────────────────────────────────────────╯
```

`testdata/claude_idle.txt`:

```
● Done. The test now passes.

╭──────────────────────────────────────────────╮
│ ❯                                             │
╰──────────────────────────────────────────────╯
  ? for shortcuts
```

`testdata/claude_dialog.txt`:

```
Bash command
  rm -rf build
Do you want to proceed?
❯ 1. Yes
  2. Yes, and don't ask again for rm commands
  3. No, and tell Claude what to do differently
  Enter to confirm · Esc to cancel
```

Test:

```go
package activity

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestClassifyScreen(t *testing.T) {
	cases := map[string]State{
		"claude_active.txt": Active,
		"claude_idle.txt":   Idle,
		"claude_dialog.txt": WaitingInput,
	}
	for name, want := range cases {
		if got := ClassifyScreen(fixture(t, name)); got != want {
			t.Errorf("%s: got %q want %q", name, got, want)
		}
	}
	if got := ClassifyScreen("random shell output\n$ "); got != "" {
		t.Errorf("unknown screen classified as %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/activity -run ClassifyScreen -v`
Expected: compile error.

- [ ] **Step 3: Write minimal implementation**

```go
package activity

import (
	"regexp"
	"strings"
)

var (
	optionLine  = regexp.MustCompile(`^\s*(❯\s*)?\d+\.\s`)
	hintLine    = regexp.MustCompile(`(?i)(enter to confirm|enter to select|esc to cancel|tab to amend)`)
	spinnerLine = regexp.MustCompile(`^[✢✳✶✻✽●·]`)
)

func ClassifyScreen(text string) State {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > 16 {
		lines = lines[len(lines)-16:]
	}
	if dialogVisible(lines) {
		return WaitingInput
	}
	prompt := lastIndex(lines, func(l string) bool { return strings.HasPrefix(strings.TrimLeft(l, "│ "), "❯") })
	if prompt < 0 {
		return ""
	}
	for i := prompt - 1; i >= 0 && i >= prompt-4; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" || strings.HasPrefix(l, "╭") || strings.HasPrefix(l, "╰") || strings.HasPrefix(l, "─") {
			continue
		}
		if spinnerLine.MatchString(l) && strings.Contains(strings.ToLower(l), "esc to interrupt") {
			return Active
		}
		break
	}
	return Idle
}

func dialogVisible(lines []string) bool {
	options := 0
	hint := false
	for _, l := range lines {
		if optionLine.MatchString(l) {
			options++
		}
		if hintLine.MatchString(l) {
			hint = true
		}
	}
	return options >= 2 && hint
}

func lastIndex(lines []string, pred func(string) bool) int {
	for i := len(lines) - 1; i >= 0; i-- {
		if pred(lines[i]) {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/activity -run ClassifyScreen -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/activity
git commit -m "feat(activity): structural screen classifier for the reconciler"
```

### Task 20: Reconciler loop

**Files:**
- Create: `internal/activity/reconcile.go`
- Test: `internal/activity/reconcile_test.go`

**Interfaces:**
- Produces:

```go
type Capturer func(sessionID string) (string, error)

type Reconciler struct{ ... }
func NewReconciler(svc *Service, capture Capturer, tick, staleAfter time.Duration) *Reconciler
func (r *Reconciler) Run(ctx context.Context)
func (r *Reconciler) Once(ctx context.Context) // one pass, for tests
```

Rule: for each session in `Snapshot()` with `State == Active` and `now - Since >= staleAfter`, capture, classify, and if the result is non-empty apply `Signal{Event: "terminal-<state>", State: result}`. The machine already refuses terminal signals on sticky states, and the classifier never emits `Blocked`.

- [ ] **Step 1: Write the failing test**

```go
package activity

import (
	"context"
	"testing"
	"time"
)

func TestReconcilerDemotesStaleActive(t *testing.T) {
	svc := NewService(newMemStore(), NewHub())
	base := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }
	svc.Register("S1", "AGT-01", "")
	svc.Register("S2", "AGT-02", "")
	_ = svc.Apply(context.Background(), "S2", Signal{Event: "permission-request", State: Blocked, ToolName: "Bash"})

	captured := map[string]int{}
	capture := func(id string) (string, error) {
		captured[id]++
		return "● done\n╭──╮\n│ ❯  │\n╰──╯\n", nil
	}
	r := NewReconciler(svc, capture, time.Hour, 2*time.Minute)

	r.Once(context.Background())
	if svc.Snapshot()["S1"].State != Active {
		t.Fatal("reconciled before staleAfter")
	}

	svc.now = func() time.Time { return base.Add(3 * time.Minute) }
	r.Once(context.Background())
	if got := svc.Snapshot()["S1"].State; got != Idle {
		t.Fatalf("S1 = %s want idle", got)
	}
	if svc.Snapshot()["S2"].State != Blocked {
		t.Fatal("reconciler touched a blocked session")
	}
	if captured["S2"] != 0 {
		t.Fatal("reconciler captured a non-active session")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/activity -run Reconciler -v`
Expected: compile error.

- [ ] **Step 3: Write minimal implementation**

```go
package activity

import (
	"context"
	"time"
)

type Capturer func(sessionID string) (string, error)

type Reconciler struct {
	svc        *Service
	capture    Capturer
	tick       time.Duration
	staleAfter time.Duration
}

func NewReconciler(svc *Service, capture Capturer, tick, staleAfter time.Duration) *Reconciler {
	return &Reconciler{svc: svc, capture: capture, tick: tick, staleAfter: staleAfter}
}

func (r *Reconciler) Run(ctx context.Context) {
	t := time.NewTicker(r.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.Once(ctx)
		}
	}
}

func (r *Reconciler) Once(ctx context.Context) {
	now := r.svc.now()
	for id, info := range r.svc.Snapshot() {
		if info.State != Active || now.Sub(info.Since) < r.staleAfter {
			continue
		}
		screen, err := r.capture(id)
		if err != nil {
			continue
		}
		state := ClassifyScreen(screen)
		if state == "" || state == Blocked {
			continue
		}
		_ = r.svc.Apply(ctx, id, Signal{Event: "terminal-" + string(state), State: state})
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/activity -race -v 2>&1 | tail -8`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/activity
git commit -m "feat(activity): stale-state reconciler over capture-pane"
```

### Task 21: Wire the reconciler and expose no_signal in the TUI

**Files:**
- Modify: `cmd/agentboard/run.go`
- Modify: `internal/orchestrator/service.go` (`CapturePaneText(sessionID) (string, error)`)
- Modify: `internal/tui/app.go` (`refreshActivity` maps `Active && !EverSignalled && since > 90s` to `no_signal` for Claude sessions)
- Test: `internal/tui/app_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRefreshActivityMarksNoSignal(t *testing.T) {
	a := newTestApp(t)
	hub := activity.NewHub()
	as := activity.NewService(nil, hub)
	a.orchestrator.(*fakeOrchestrator).activity = as
	as.Register("S1", "AGT-01", "L1")
	a.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	a.refreshActivity()
	if a.activity["AGT-01"] != "no_signal" {
		t.Fatalf("activity = %v", a.activity)
	}
}
```

`fakeOrchestrator.ActivityService()` returns its `activity` field. Add `now func() time.Time` to `App`, defaulting to `time.Now` in `NewApp`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run NoSignal -v`
Expected: FAIL, activity is `active`.

- [ ] **Step 3: Write minimal implementation**

In `refreshActivity`:

```go
	const noSignalGrace = 90 * time.Second
	for _, info := range as.Snapshot() {
		if info.TicketID == "" {
			continue
		}
		state := string(info.State)
		if info.State == activity.Active && !info.EverSignalled && a.now().Sub(info.Since) > noSignalGrace {
			state = "no_signal"
		}
		a.activity[info.TicketID] = state
	}
```

`orchestrator/service.go`:

```go
func (s *Service) CapturePaneText(sessionID string) (string, error) {
	if s.agentRunner == nil {
		return "", fmt.Errorf("orchestrator.capturePaneText: no agent runner")
	}
	paneID, ok := s.agentRunner.GetPaneID(sessionID)
	if !ok {
		return "", fmt.Errorf("orchestrator.capturePaneText: no pane for %s", sessionID)
	}
	out, err := exec.Command("tmux", "capture-pane", "-t", paneID, "-p", "-J", "-S", "-40").Output()
	if err != nil {
		return "", fmt.Errorf("orchestrator.capturePaneText: %w", err)
	}
	return pty.StripANSI(string(out)), nil
}
```

`run.go`, after the notifier:

```go
	go activity.NewReconciler(actSvc, orch.CapturePaneText, 30*time.Second, 2*time.Minute).Run(appCtx)
```

- [ ] **Step 4: Run tests and build**

Run: `go build ./... && go test ./... 2>&1 | tail -15 && go vet ./...`
Expected: all PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
git add cmd/agentboard/run.go internal/orchestrator/service.go internal/tui
git commit -m "feat: wire reconciler; show no_signal for silent claude sessions"
```

### Task 22: Documentation

**Files:**
- Modify: `CLAUDE.md` (Architecture Summary, Folder Structure, Keybinding Reference, new "Agent Activity Tracking" section after "Git Worktree Isolation & Merge Flow")

- [ ] **Step 1: Write the section**

Add to the folder structure listing:

```
│   ├── activity/           # Agent activity tracking
│   │   ├── state.go        # State enum and predicates
│   │   ├── machine.go      # Transition function, tool-flight precedence
│   │   ├── claude.go       # Claude Code hook mapper, --settings builder
│   │   ├── hub.go          # In-process event hub
│   │   ├── service.go      # Apply signals, pending requests, snapshot
│   │   ├── receiver.go     # POST /hooks/{sessionID} loopback server
│   │   ├── notify.go       # notify-send on entry into needs-input
│   │   ├── screen.go       # Structural classifier over capture-pane text
│   │   └── reconcile.go    # Stale-state reconciler
```

Add the keybinding row `| w | Open the waiting agents overlay (y allow, n deny, Enter jump) |`.

Add the section:

```markdown
## Agent Activity Tracking

Every spawned agent reports its own state through hooks; the board never guesses from the screen except as a stale fallback.

States: `active`, `idle`, `waiting_input` (question asked, safe to type), `blocked` (permission dialog open, never type), `exited`. TUI also shows `no_signal` for a Claude session that has sent no hook in 90 s.

Flow: `Service.StartAdHocRun` generates a launch id, exports `AGENTBOARD_SESSION_ID`, `AGENTBOARD_LAUNCH_ID`, `AGENTBOARD_HOOK_URL` into the tmux window, and for Claude Code appends `--settings` with http hooks pointing at `http://127.0.0.1:<port>/hooks/<sessionID>`. The receiver maps hook payloads to signals, drops signals with a stale launch id, and applies them through `activity.Apply`. `PermissionRequest` is held open until the user answers in the `w` overlay or the 10 minute timeout, in which case Claude falls back to its own pane prompt.

Guards: while `blocked`, only a turn boundary or the `PostToolUse` of the blocking tool clears the state. `SendInput` refuses on `blocked` and `exited`. The reconciler (30 s tick, 2 min stale) may demote `active` to `idle` or `waiting_input` but never writes `blocked` or clears a sticky state.

Notifications: `notify-send` fires once on entry into `waiting_input` or `blocked`, skipped when the board is focused and the ticket is visible.

Not yet covered: Cursor, Codex, mobile push. OpenCode is covered by its own plugin (spec phase 5). See `docs/superpowers/specs/2026-09-08-agent-activity-tracking-design.md`.
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document agent activity tracking and the w overlay"
```

---

## Phase 5: FreeCode

### Task 23: FreeCode carries the tool-use id into hook context

**Files (freecode repo):**
- Modify: `apps/core/src/agent/loop.ts:2440-2444`
- Modify: `apps/core/src/hooks/executors/command.ts` (env export, next to `CLAUDE_TOOL_NAME`)
- Test: `apps/core/src/hooks/hook-payloads.test.ts`

**Interfaces:**
- Produces: `HookContext.toolUseId: string` present on `PreToolUse` and `PostToolUse` (the `HookContext` type already has an index signature, so no type change). Shell hooks also see `CLAUDE_TOOL_USE_ID`.

- [ ] **Step 1: Write the failing test**

Open `apps/core/src/hooks/hook-payloads.test.ts` and look at how it builds a context and asserts env for the command executor. Add:

```ts
test("command executor exports CLAUDE_TOOL_USE_ID when the context carries toolUseId", () => {
  const env = buildHookEnv(
    { toolName: "bash", toolInput: { command: "ls" } },
    { sessionId: "s1", turnCount: 1, toolName: "bash", toolUseId: "call_42" },
  );
  assert.equal(env.CLAUDE_TOOL_USE_ID, "call_42");
});
```

If `buildHookEnv` is not exported from `executors/command.ts`, extract the env-building block at lines 56-71 into an exported `buildHookEnv(input, context)` as part of this task.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/ayande/Project/freecode && pnpm --filter @freecode/core test -- --test-name-pattern "CLAUDE_TOOL_USE_ID"`
Expected: FAIL, `undefined !== "call_42"`.

- [ ] **Step 3: Write minimal implementation**

`agent/loop.ts`:

```ts
    const hookContext: HookContext = {
      sessionId: this.state.sessionId,
      turnCount: this.state.turnCount,
      toolName: toolCall.tool,
      toolUseId: toolCall.id,
    };
```

`executors/command.ts`, inside the env block:

```ts
  if (typeof context.toolUseId === "string") env.CLAUDE_TOOL_USE_ID = context.toolUseId;
```

- [ ] **Step 4: Run tests**

Run: `cd /home/ayande/Project/freecode && pnpm --filter @freecode/core test 2>&1 | tail -5 && pnpm typecheck 2>&1 | tail -3`
Expected: PASS, typecheck clean.

- [ ] **Step 5: Commit (freecode repo)**

```bash
cd /home/ayande/Project/freecode
git add apps/core/src/agent/loop.ts apps/core/src/hooks/executors/command.ts apps/core/src/hooks/hook-payloads.test.ts
git commit -m "feat(hooks): carry toolUseId into hook context and CLAUDE_TOOL_USE_ID"
```

### Task 24: FreeCode board webhook, pure builders

**Files (freecode repo):**
- Create: `apps/core/src/hooks/builtin/board-webhook.ts`
- Test: `apps/core/src/hooks/builtin/board-webhook.test.ts`

**Interfaces:**
- Produces:

```ts
export type BoardPayload = {
  hook_event_name: "SessionStart" | "UserPromptSubmit" | "PreToolUse" | "PostToolUse" | "Stop" | "PermissionRequest" | "Notification";
  session_id?: string;
  tool_name?: string;
  tool_use_id?: string;
  tool_input?: Record<string, unknown>;
  notification_type?: "agent_needs_input";
};

// Maps a FreeCode hook event to the Claude-shaped payload the board expects. Returns null for events the board ignores.
export function hookEventToPayload(event: HookEventName, input: ToolCallInput, ctx: HookContext): BoardPayload | null;
export function permissionToPayload(e: PermissionAskedEvent): BoardPayload;
export function questionToPayload(e: QuestionAskedEvent): BoardPayload;
// Maps the board's reply to a FreeCode answer. Returns null when the board did not decide.
export function replyToAnswer(reply: unknown): PermissionAnswer | "reject" | null;
export function registerBoardWebhook(): void;
```

- [ ] **Step 1: Write the failing test**

```ts
import test from "node:test";
import assert from "node:assert/strict";
import {
  hookEventToPayload,
  permissionToPayload,
  questionToPayload,
  replyToAnswer,
} from "./board-webhook.js";

const ctx = { sessionId: "s1", turnCount: 2, toolName: "bash", toolUseId: "call_1" };
const input = { toolName: "bash", toolInput: { command: "ls" } };

test("TurnStart is reported to the board as UserPromptSubmit", () => {
  assert.deepEqual(hookEventToPayload("TurnStart", input, ctx), {
    hook_event_name: "UserPromptSubmit",
    session_id: "s1",
  });
});

test("PreToolUse and PostToolUse carry tool name, id and input", () => {
  for (const ev of ["PreToolUse", "PostToolUse"] as const) {
    assert.deepEqual(hookEventToPayload(ev, input, ctx), {
      hook_event_name: ev,
      session_id: "s1",
      tool_name: "bash",
      tool_use_id: "call_1",
      tool_input: { command: "ls" },
    });
  }
});

test("SessionStart and Stop map directly; TurnEnd and UserPromptSubmit are ignored", () => {
  assert.equal(hookEventToPayload("SessionStart", input, ctx)?.hook_event_name, "SessionStart");
  assert.equal(hookEventToPayload("Stop", input, ctx)?.hook_event_name, "Stop");
  assert.equal(hookEventToPayload("TurnEnd", input, ctx), null);
  assert.equal(hookEventToPayload("UserPromptSubmit", input, ctx), null);
  assert.equal(hookEventToPayload("PreCompact", input, ctx), null);
});

test("permission.asked becomes PermissionRequest", () => {
  assert.deepEqual(
    permissionToPayload({
      type: "permission.asked",
      requestId: "r1",
      sessionId: "s1",
      toolName: "bash",
      args: { command: "rm -rf build" },
      description: "Run rm -rf build",
    }),
    {
      hook_event_name: "PermissionRequest",
      session_id: "s1",
      tool_name: "bash",
      tool_input: { command: "rm -rf build" },
    },
  );
});

test("question.asked becomes Notification agent_needs_input", () => {
  assert.deepEqual(
    questionToPayload({ type: "question.asked", requestId: "q1", sessionId: "s1", questions: [] }),
    { hook_event_name: "Notification", session_id: "s1", notification_type: "agent_needs_input" },
  );
});

test("replyToAnswer maps allow, deny, and silence", () => {
  assert.deepEqual(replyToAnswer({ hookSpecificOutput: { decision: "allow" } }), { decision: "allow-once" });
  assert.equal(replyToAnswer({ hookSpecificOutput: { decision: "deny" } }), "reject");
  assert.equal(replyToAnswer({}), null);
  assert.equal(replyToAnswer(undefined), null);
  assert.equal(replyToAnswer({ hookSpecificOutput: { decision: "maybe" } }), null);
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/ayande/Project/freecode && pnpm --filter @freecode/core test -- --test-name-pattern "board"`
Expected: module not found.

- [ ] **Step 3: Write minimal implementation**

```ts
// =============================================================================
// board-webhook — reports agent activity to AgentBoard over HTTP and lets the
// board answer permission requests remotely. Inert unless FREECODE_HOOK_URL is
// set. Payloads use the Claude Code hook JSON shape so the board needs no
// FreeCode-specific mapper.
// =============================================================================

import { registerHook } from "../registry.js";
import type { HookContext, HookEventName, HookResult, ToolCallInput } from "../types.js";
import {
  bus,
  answerPermission,
  rejectPermission,
  type PermissionAnswer,
  type PermissionAskedEvent,
  type QuestionAskedEvent,
} from "../../bus/index.js";

export type BoardPayload = {
  hook_event_name:
    | "SessionStart"
    | "UserPromptSubmit"
    | "PreToolUse"
    | "PostToolUse"
    | "Stop"
    | "PermissionRequest"
    | "Notification";
  session_id?: string;
  tool_name?: string;
  tool_use_id?: string;
  tool_input?: Record<string, unknown>;
  notification_type?: "agent_needs_input";
};

const FORWARDED: HookEventName[] = ["SessionStart", "TurnStart", "PreToolUse", "PostToolUse", "Stop"];
const PERMISSION_WAIT_MS = 30 * 60 * 1000;
const EVENT_TIMEOUT_MS = 2000;

export function hookEventToPayload(
  event: HookEventName,
  input: ToolCallInput,
  ctx: HookContext,
): BoardPayload | null {
  const session_id = ctx.sessionId;
  switch (event) {
    case "SessionStart":
      return { hook_event_name: "SessionStart", session_id };
    case "TurnStart":
      return { hook_event_name: "UserPromptSubmit", session_id };
    case "Stop":
      return { hook_event_name: "Stop", session_id };
    case "PreToolUse":
    case "PostToolUse":
      return {
        hook_event_name: event,
        session_id,
        tool_name: input.toolName,
        tool_use_id: typeof ctx.toolUseId === "string" ? ctx.toolUseId : undefined,
        tool_input: input.toolInput,
      };
    default:
      return null;
  }
}

export function permissionToPayload(e: PermissionAskedEvent): BoardPayload {
  return {
    hook_event_name: "PermissionRequest",
    session_id: e.sessionId,
    tool_name: e.toolName,
    tool_input: e.args,
  };
}

export function questionToPayload(e: QuestionAskedEvent): BoardPayload {
  return { hook_event_name: "Notification", session_id: e.sessionId, notification_type: "agent_needs_input" };
}

export function replyToAnswer(reply: unknown): PermissionAnswer | "reject" | null {
  const decision = (reply as { hookSpecificOutput?: { decision?: unknown } } | undefined)?.hookSpecificOutput
    ?.decision;
  if (decision === "allow") return { decision: "allow-once" };
  if (decision === "deny") return "reject";
  return null;
}

function hookUrl(): string | undefined {
  return process.env.FREECODE_HOOK_URL;
}

async function post(body: BoardPayload, timeoutMs: number): Promise<unknown> {
  const url = hookUrl();
  if (!url) return undefined;
  const ac = new AbortController();
  const timer = setTimeout(() => ac.abort(), timeoutMs);
  try {
    const res = await fetch(url, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "x-agentboard-launch": process.env.AGENTBOARD_LAUNCH_ID ?? "",
      },
      body: JSON.stringify(body),
      signal: ac.signal,
    });
    if (!res.ok) return undefined;
    return await res.json().catch(() => undefined);
  } catch {
    return undefined;
  } finally {
    clearTimeout(timer);
  }
}

export function registerBoardWebhook(): void {
  if (!hookUrl()) return;

  for (const event of FORWARDED) {
    registerHook(
      event,
      `board-webhook-${event}`,
      {
        type: "callback",
        internal: true,
        callback: async (input: ToolCallInput, ctx: HookContext): Promise<HookResult> => {
          const payload = hookEventToPayload(event, input, ctx);
          if (payload) void post(payload, EVENT_TIMEOUT_MS);
          return { action: "continue" };
        },
      },
      "settings",
    );
  }

  bus.subscribe("permission.asked", (e) => {
    void (async () => {
      const answer = replyToAnswer(await post(permissionToPayload(e), PERMISSION_WAIT_MS));
      if (answer === null) return;
      if (answer === "reject") rejectPermission(e.requestId);
      else answerPermission(e.requestId, answer);
    })();
  });

  bus.subscribe("question.asked", (e) => {
    void post(questionToPayload(e), EVENT_TIMEOUT_MS);
  });
}
```

Check the `bus.subscribe` signature in `apps/core/src/bus/index.ts` and adapt the callback typing if it takes `(type, handler)`.

- [ ] **Step 4: Run tests**

Run: `cd /home/ayande/Project/freecode && pnpm --filter @freecode/core test 2>&1 | tail -5 && pnpm typecheck 2>&1 | tail -3`
Expected: PASS, typecheck clean.

- [ ] **Step 5: Commit (freecode repo)**

```bash
cd /home/ayande/Project/freecode
git add apps/core/src/hooks/builtin/board-webhook.ts apps/core/src/hooks/builtin/board-webhook.test.ts
git commit -m "feat(hooks): board-webhook reports activity and accepts remote permission decisions"
```

### Task 25: Register the webhook at bootstrap

**Files (freecode repo):**
- Modify: `apps/core/src/hooks/bootstrap.ts`
- Test: `apps/core/src/hooks/bootstrap.test.ts`

- [ ] **Step 1: Write the failing test**

Look at how `bootstrap.test.ts` inspects registered hooks (it likely uses `getHooksForEvent` or similar from `registry.ts`). Add:

```ts
test("initHooks registers board-webhook hooks when FREECODE_HOOK_URL is set", () => {
  process.env.FREECODE_HOOK_URL = "http://127.0.0.1:1/hooks/SES-01";
  try {
    const mgr = initHooks(tmpProject());
    mgr.dispose?.();
    const names = getHooksForEvent("Stop").map((h) => h.name);
    assert.ok(names.includes("board-webhook-Stop"), names.join(","));
  } finally {
    delete process.env.FREECODE_HOOK_URL;
    unregisterAllHooks("settings");
  }
});

test("initHooks registers no board-webhook hooks without FREECODE_HOOK_URL", () => {
  delete process.env.FREECODE_HOOK_URL;
  const mgr = initHooks(tmpProject());
  mgr.dispose?.();
  const names = getHooksForEvent("Stop").map((h) => h.name);
  assert.ok(!names.some((n) => n.startsWith("board-webhook-")), names.join(","));
  unregisterAllHooks("settings");
});
```

Use the existing temp-project helper in that test file for `tmpProject()`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/ayande/Project/freecode && pnpm --filter @freecode/core test -- --test-name-pattern "board-webhook"`
Expected: FAIL, hook name absent.

- [ ] **Step 3: Write minimal implementation**

```ts
import { registerBoardWebhook } from "./builtin/board-webhook.js";
...
  registerRtkHook();
  registerBoardWebhook();
```

- [ ] **Step 4: Run tests**

Run: `cd /home/ayande/Project/freecode && pnpm --filter @freecode/core test 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 5: Commit (freecode repo)**

```bash
cd /home/ayande/Project/freecode
git add apps/core/src/hooks/bootstrap.ts apps/core/src/hooks/bootstrap.test.ts
git commit -m "feat(hooks): register board-webhook at bootstrap"
```

### Task 26: Board exports FREECODE_HOOK_URL for freecode sessions

**Files:**
- Modify: `internal/orchestrator/service.go` (`launchEnv`)
- Test: `internal/orchestrator/service_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestStartAdHocRunFreecodeGetsHookURL(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	fr := &capturingRunner{}
	svc := NewService(fs, nil, nil, nil)
	svc.SetAgentRunner(fr)
	svc.SetActivity(&fakeActivity{}, "http://127.0.0.1:9")
	sess, err := svc.StartAdHocRun(context.Background(), "AGT-01", "freecode", "x")
	if err != nil {
		t.Fatal(err)
	}
	env := strings.Join(fr.last.Env, "\n")
	if !strings.Contains(env, "FREECODE_HOOK_URL=http://127.0.0.1:9/hooks/"+sess.ID) {
		t.Fatalf("env = %v", fr.last.Env)
	}
	if len(fr.last.ExtraArgs) != 0 {
		t.Fatalf("freecode should get no ExtraArgs, got %v", fr.last.ExtraArgs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/orchestrator -run FreecodeGetsHookURL -v`
Expected: FAIL, env missing.

- [ ] **Step 3: Write minimal implementation**

Change `launchEnv` to take the agent and session:

```go
func (s *Service) launchEnv(agent, sessionID, launchID string) []string {
	env := []string{
		"AGENTBOARD_SESSION_ID=" + sessionID,
		"AGENTBOARD_LAUNCH_ID=" + launchID,
	}
	if s.hookBaseURL == "" {
		return env
	}
	env = append(env, "AGENTBOARD_HOOK_URL="+s.hookBaseURL)
	if agent == "freecode" {
		env = append(env, "FREECODE_HOOK_URL="+s.hookBaseURL+"/hooks/"+sessionID)
	}
	return env
}
```

Update the two call sites from Task 13 to `s.launchEnv(agent, session.ID, launchID)`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/orchestrator 2>&1 | tail -5 && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Smoke test end to end**

Build FreeCode (`cd /home/ayande/Project/freecode && pnpm build`) so the `freecode` binary on PATH includes the webhook. Run the board inside tmux, start a `freecode` ticket, ask it to run a shell command that needs approval, and confirm the card shows `!`, `notify-send` fires, and pressing `y` in the `w` overlay approves it in the pane.

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/service.go internal/orchestrator/service_test.go
git commit -m "feat(orchestrator): export FREECODE_HOOK_URL for freecode sessions"
```

Add one line to the CLAUDE.md section from Task 22: "FreeCode: enabled by `FREECODE_HOOK_URL`, which the board exports automatically; the webhook lives in FreeCode at `apps/core/src/hooks/builtin/board-webhook.ts`."

---

## Self-review

**Spec coverage.** Activity state and store columns: Tasks 5, 9. Launch generation: Tasks 10, 13. Hook receiver with held PermissionRequest: Task 11. Claude mapper table and tool-flight rule: Tasks 6, 7. Notifier entry-only policy with visibility: Tasks 12, 14. Resolve path and InputGuard: Tasks 17, 18. Reconciler and no_signal: Tasks 19 to 21. TUI glyph, header, overlay, jump: Tasks 16, 17. Spawn env and `--settings`: Task 13. Blocking bugs 1 to 4: Tasks 1 to 4. FreeCode section: Tasks 23 to 26. Not covered by design: OpenCode server mode, Cursor, Codex, WebSocket, notification click-to-focus (spec lists the last as an open question; body names the ticket instead).

**Type consistency.** `RunHandle.Stop func()` (Task 2) is consumed in Task 3. `AgentRunner.Kill` (Task 3) is used by `StopSession`. `activity.Store` (Task 10) matches the store methods from Task 9. `Service.Hub()` is added in Task 16 and consumed there. `Info.EverSignalled` and `Since` (Task 10) are consumed by Tasks 20 and 21. `ActivityService()` is the single accessor name used in Tasks 14, 17, 18, 21. `fakeAgentRunner` (Task 3) is embedded by `capturingRunner` (Task 13).

**Placeholder scan.** None found.
