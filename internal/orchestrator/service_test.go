package orchestrator_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ayan-de/agent-board/internal/activity"
	"github.com/ayan-de/agent-board/internal/orchestrator"
	"github.com/ayan-de/agent-board/internal/store"
)

func TestServiceCreateProposalRequiresAssignedAgent(t *testing.T) {
	svc := orchestrator.Service{}
	_, err := svc.CreateProposal(context.Background(), orchestrator.CreateProposalInput{
		TicketID: "AGT-01",
	})
	if err == nil {
		t.Fatal("expected error when no store configured")
	}
}

func TestStartApprovedRunRejectsExistingActiveSession(t *testing.T) {
	fs := &fakeStore{
		activeSession: true,
		proposal: store.Proposal{
			ID:       "PRO-01",
			TicketID: "AGE-01",
			Agent:    "opencode",
			Status:   "approved",
			Prompt:   "do work",
		},
	}
	svc := orchestrator.NewService(fs, nil, nil, fakeCtx{})

	_, err := svc.StartApprovedRun(context.Background(), "PRO-01")
	if err == nil {
		t.Fatal("expected duplicate active session error")
	}
}

func TestStartApprovedRunCallsFinishRunForBlockingRunner(t *testing.T) {
	fs := &fakeStore{
		ticket: store.Ticket{
			ID:     "AGE-01",
			Status: "in_progress",
		},
		proposal: store.Proposal{
			ID:       "PRO-01",
			TicketID: "AGE-01",
			Agent:    "opencode",
			Status:   "approved",
			Prompt:   "do work",
		},
	}
	fllm := &fakeLLMClient{summary: "summary of run"}
	tmuxRunner := &fakeTmuxRunner{outcome: "completed", summary: "raw worker output"}
	svc := orchestrator.NewService(fs, fllm, nil, fakeCtx{})
	svc.SetAgentRunner(tmuxRunner)

	_, err := svc.StartApprovedRun(context.Background(), "PRO-01")
	if err != nil {
		t.Fatal(err)
	}

	if fs.lastMoveStatus != "review" {
		t.Fatalf("MoveStatus = %q, want review (FinishRun should have been called)", fs.lastMoveStatus)
	}
	if fs.lastAgentActive != false {
		t.Fatal("AgentActive should be false after FinishRun")
	}
	if fs.lastContextCarry.Summary != "summary of run" {
		t.Fatalf("ContextCarry.Summary = %q, want %q", fs.lastContextCarry.Summary, "summary of run")
	}
}

func TestStartApprovedRunCallsFinishRunViaAsyncOnComplete(t *testing.T) {
	fs := &fakeStore{
		ticket: store.Ticket{
			ID:     "AGE-01",
			Status: "in_progress",
		},
		proposal: store.Proposal{
			ID:       "PRO-01",
			TicketID: "AGE-01",
			Agent:    "opencode",
			Status:   "approved",
			Prompt:   "do work",
		},
	}
	fllm := &fakeLLMClient{summary: "summary of async run"}
	tmuxRunner := &fakeAsyncTmuxRunner{}
	svc := orchestrator.NewService(fs, fllm, nil, fakeCtx{})
	svc.SetAgentRunner(tmuxRunner)

	_, err := svc.StartApprovedRun(context.Background(), "PRO-01")
	if err != nil {
		t.Fatal(err)
	}

	if fs.lastMoveStatus != "" {
		t.Fatal("FinishRun should NOT have been called yet for non-blocking runner")
	}

	tmuxRunner.onComplete("completed", "async worker output", "")

	if fs.lastMoveStatus != "review" {
		t.Fatalf("MoveStatus = %q, want review after OnComplete fires", fs.lastMoveStatus)
	}
	if fs.lastContextCarry.Summary != "summary of async run" {
		t.Fatalf("ContextCarry.Summary = %q, want %q", fs.lastContextCarry.Summary, "summary of async run")
	}
}

func TestFinishRunPersistsContextCarry(t *testing.T) {
	fs := &fakeStore{
		ticket: store.Ticket{
			ID:     "AGE-01",
			Status: "in_progress",
		},
	}
	fllm := &fakeLLMClient{summary: "short handoff summary"}
	svc := orchestrator.NewService(fs, fllm, nil, fakeCtx{})

	err := svc.FinishRun(context.Background(), orchestrator.FinishRunInput{
		TicketID:  "AGE-01",
		SessionID: "SES-01",
		Outcome:   "completed",
		Summary:   "raw worker summary",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fs.lastContextCarry.Summary != "short handoff summary" {
		t.Fatalf("Summary = %q, want short handoff summary", fs.lastContextCarry.Summary)
	}
}

func envValue(env []string, key string) (string, bool) {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return strings.TrimPrefix(kv, key+"="), true
		}
	}
	return "", false
}

func TestStartAdHocRunExportsSessionEnv(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	cr := &capturingRunner{}
	svc := orchestrator.NewService(fs, nil, nil, nil)
	svc.SetAgentRunner(cr)
	svc.SetHookBaseURL("http://127.0.0.1:9999")

	sess, err := svc.StartAdHocRun(context.Background(), "AGT-01", "claude-code", "do it")
	if err != nil {
		t.Fatalf("StartAdHocRun() error = %v", err)
	}

	if got, _ := envValue(cr.last.Env, "AGENTBOARD_SESSION_ID"); got != sess.ID {
		t.Errorf("AGENTBOARD_SESSION_ID = %q, want %q", got, sess.ID)
	}
	if got, ok := envValue(cr.last.Env, "AGENTBOARD_LAUNCH_ID"); !ok || got == "" {
		t.Error("AGENTBOARD_LAUNCH_ID missing or empty")
	}
	if got, _ := envValue(cr.last.Env, "AGENTBOARD_HOOK_URL"); got != "http://127.0.0.1:9999" {
		t.Errorf("AGENTBOARD_HOOK_URL = %q", got)
	}
	if _, ok := envValue(cr.last.Env, "FREECODE_HOOK_URL"); ok {
		t.Error("non-freecode agent should not get FREECODE_HOOK_URL")
	}
}

func TestStartAdHocRunFreecodeGetsHookURL(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	cr := &capturingRunner{}
	svc := orchestrator.NewService(fs, nil, nil, nil)
	svc.SetAgentRunner(cr)
	svc.SetHookBaseURL("http://127.0.0.1:9999/")

	sess, err := svc.StartAdHocRun(context.Background(), "AGT-01", "freecode", "do it")
	if err != nil {
		t.Fatalf("StartAdHocRun() error = %v", err)
	}

	want := "http://127.0.0.1:9999/hooks/" + sess.ID
	if got, _ := envValue(cr.last.Env, "FREECODE_HOOK_URL"); got != want {
		t.Errorf("FREECODE_HOOK_URL = %q, want %q", got, want)
	}
}

func TestStartAdHocRunNonReportingAgentGetsNoSettings(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	cr := &capturingRunner{}
	svc := orchestrator.NewService(fs, nil, nil, nil)
	svc.SetAgentRunner(cr)
	svc.SetHookBaseURL("http://127.0.0.1:9999")

	launched := false
	svc.SetLaunchNotifier(func(string, string, activity.DecisionCodec) { launched = true })

	if _, err := svc.StartAdHocRun(context.Background(), "AGT-01", "codex", "do it"); err != nil {
		t.Fatalf("StartAdHocRun() error = %v", err)
	}
	// codex reports nothing, so the board must neither hand it a settings
	// document nor start tracking a session that will never report.
	if cr.last.Settings != "" {
		t.Errorf("Settings = %q, want none", cr.last.Settings)
	}
	if launched {
		t.Error("launch notifier fired for an agent that cannot report")
	}
}

func TestStartAdHocRunNotifiesLaunchWithTheAgentsDecisionCodec(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	cr := &capturingRunner{}
	svc := orchestrator.NewService(fs, nil, nil, nil)
	svc.SetAgentRunner(cr)
	svc.SetHookBaseURL("http://127.0.0.1:9999")

	var codec activity.DecisionCodec
	svc.SetLaunchNotifier(func(_, _ string, c activity.DecisionCodec) { codec = c })

	if _, err := svc.StartAdHocRun(context.Background(), "AGT-01", "freecode", "do it"); err != nil {
		t.Fatalf("StartAdHocRun() error = %v", err)
	}
	if codec == nil {
		t.Fatal("freecode launched with no decision codec")
	}
	if got := codec("allow"); got != "allow" {
		t.Errorf("freecode codec produced %#v, want the bare string", got)
	}
}

func TestStartAdHocRunWithoutHookURLExportsNoHookEnv(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	cr := &capturingRunner{}
	svc := orchestrator.NewService(fs, nil, nil, nil)
	svc.SetAgentRunner(cr)

	if _, err := svc.StartAdHocRun(context.Background(), "AGT-01", "freecode", "do it"); err != nil {
		t.Fatalf("StartAdHocRun() error = %v", err)
	}
	for _, key := range []string{"AGENTBOARD_HOOK_URL", "FREECODE_HOOK_URL"} {
		if _, ok := envValue(cr.last.Env, key); ok {
			t.Errorf("%s exported with no hook receiver configured", key)
		}
	}
	if _, ok := envValue(cr.last.Env, "AGENTBOARD_SESSION_ID"); !ok {
		t.Error("AGENTBOARD_SESSION_ID should always be exported")
	}
}

func TestStartAdHocRunInstallsClaudeHookSettings(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	cr := &capturingRunner{}
	svc := orchestrator.NewService(fs, nil, nil, nil)
	svc.SetAgentRunner(cr)
	svc.SetHookBaseURL("http://127.0.0.1:9999")

	sess, err := svc.StartAdHocRun(context.Background(), "AGT-01", "claude-code", "do it")
	if err != nil {
		t.Fatalf("StartAdHocRun() error = %v", err)
	}

	want := "http://127.0.0.1:9999/hooks/" + sess.ID
	if !strings.Contains(cr.last.Settings, want) {
		t.Errorf("settings do not point at %s: %s", want, cr.last.Settings)
	}
	if !strings.Contains(cr.last.Settings, `"PermissionRequest"`) {
		t.Errorf("settings register no PermissionRequest hook: %s", cr.last.Settings)
	}
}

func TestStartAdHocRunWithoutHookURLBuildsNoSettings(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	cr := &capturingRunner{}
	svc := orchestrator.NewService(fs, nil, nil, nil)
	svc.SetAgentRunner(cr)

	if _, err := svc.StartAdHocRun(context.Background(), "AGT-01", "claude-code", "do it"); err != nil {
		t.Fatalf("StartAdHocRun() error = %v", err)
	}
	if cr.last.Settings != "" {
		t.Errorf("Settings = %s, want none with no receiver running", cr.last.Settings)
	}
}

func TestStartAdHocRunLeavesOtherAgentsUnflagged(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	cr := &capturingRunner{}
	svc := orchestrator.NewService(fs, nil, nil, nil)
	svc.SetAgentRunner(cr)
	svc.SetHookBaseURL("http://127.0.0.1:9999")

	if _, err := svc.StartAdHocRun(context.Background(), "AGT-01", "freecode", "do it"); err != nil {
		t.Fatalf("StartAdHocRun() error = %v", err)
	}
	if cr.last.Settings != "" {
		t.Errorf("Settings = %s, want none for a non-claude agent", cr.last.Settings)
	}
}

func TestStartAdHocRunOpencodeGetsPluginConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	cr := &capturingRunner{}
	svc := orchestrator.NewService(fs, nil, nil, nil)
	svc.SetAgentRunner(cr)
	svc.SetHookBaseURL("http://127.0.0.1:9999")

	if _, err := svc.StartAdHocRun(context.Background(), "AGT-01", "opencode", "do it"); err != nil {
		t.Fatalf("StartAdHocRun() error = %v", err)
	}
	got, ok := envValue(cr.last.Env, "OPENCODE_CONFIG")
	if !ok {
		t.Fatal("OPENCODE_CONFIG not exported for opencode")
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("OPENCODE_CONFIG points at %s, which does not exist: %v", got, err)
	}
	if cr.last.Settings != "" {
		t.Errorf("Settings = %s, want none for opencode", cr.last.Settings)
	}
}

func TestStartAdHocRunOpencodeWithoutHookURLExportsNoConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	cr := &capturingRunner{}
	svc := orchestrator.NewService(fs, nil, nil, nil)
	svc.SetAgentRunner(cr)

	if _, err := svc.StartAdHocRun(context.Background(), "AGT-01", "opencode", "do it"); err != nil {
		t.Fatalf("StartAdHocRun() error = %v", err)
	}
	if _, ok := envValue(cr.last.Env, "OPENCODE_CONFIG"); ok {
		t.Error("OPENCODE_CONFIG exported with no hook receiver configured")
	}
}

func TestCompletionsFanOutToEverySubscriber(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	runner := &fakeAsyncTmuxRunner{}
	svc := orchestrator.NewService(fs, nil, nil, nil)
	svc.SetAgentRunner(runner)

	first, cancelFirst := svc.SubscribeCompletions()
	second, cancelSecond := svc.SubscribeCompletions()
	defer cancelSecond()

	sess, err := svc.StartAdHocRun(context.Background(), "AGT-01", "opencode", "x")
	if err != nil {
		t.Fatal(err)
	}
	runner.onComplete("completed", "done", "")

	for name, ch := range map[string]<-chan orchestrator.RunCompletion{"first": first, "second": second} {
		select {
		case c := <-ch:
			if c.TicketID != "AGT-01" || c.SessionID != sess.ID || c.Outcome != "completed" {
				t.Fatalf("%s got %+v", name, c)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s subscriber never received the completion", name)
		}
	}

	cancelFirst()
	if _, open := <-first; open {
		t.Fatal("cancelled subscription was not closed")
	}
	runner.onComplete("completed", "again", "")
	select {
	case c := <-second:
		if c.Summary != "again" {
			t.Fatalf("second got %+v", c)
		}
	case <-time.After(time.Second):
		t.Fatal("surviving subscriber lost a completion after another cancelled")
	}
}

func TestCompletionNeverBlocksTheAgent(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	runner := &fakeAsyncTmuxRunner{}
	svc := orchestrator.NewService(fs, nil, nil, nil)
	svc.SetAgentRunner(runner)

	slow, cancel := svc.SubscribeCompletions()
	defer cancel()
	_ = slow

	if _, err := svc.StartAdHocRun(context.Background(), "AGT-01", "opencode", "x"); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			runner.onComplete("completed", "n", "")
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("onComplete blocked once nobody was draining completions")
	}
}
