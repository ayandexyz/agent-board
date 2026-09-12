package orchestrator_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ayan-de/agent-board/internal/orchestrator"
	"github.com/ayan-de/agent-board/internal/store"
)

type recordingAgentRunner struct {
	lastReq orchestrator.RunRequest
}

func (f *recordingAgentRunner) Start(_ context.Context, req orchestrator.RunRequest) (orchestrator.RunHandle, error) {
	f.lastReq = req
	return orchestrator.RunHandle{Outcome: "running"}, nil
}

func (f *recordingAgentRunner) GetPaneID(_ string) (string, bool) { return "", false }
func (f *recordingAgentRunner) Kill(_ string) error               { return nil }
func (f *recordingAgentRunner) SendInput(_, _ string) error       { return nil }

func TestStartAdHocRunEnsuresWorktree(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Title: "T", Status: "in_progress"}}
	runner := &recordingAgentRunner{}
	wt := &fakeWorktrees{info: orchestrator.WorktreeInfo{Path: "/wt/AGT-01", Branch: "agent/agt-01", Created: true}}

	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetAgentRunner(runner)
	svc.SetWorktrees(wt)

	_, err := svc.StartAdHocRun(context.Background(), "AGT-01", "opencode", "do it")
	if err != nil {
		t.Fatalf("StartAdHocRun: %v", err)
	}

	if runner.lastReq.WorkDir != "/wt/AGT-01" {
		t.Errorf("RunRequest.WorkDir = %q, want /wt/AGT-01", runner.lastReq.WorkDir)
	}
	if fs.lastWorktreePath != "/wt/AGT-01" {
		t.Errorf("SetWorktreePath = %q, want /wt/AGT-01", fs.lastWorktreePath)
	}
	if !fs.hasEventKind("worktree.created") {
		t.Errorf("worktree.created event missing, events = %v", fs.events)
	}
	if wt.lastTicket.ID != "AGT-01" {
		t.Errorf("Ensure ticket = %q, want AGT-01", wt.lastTicket.ID)
	}
}

func TestStartAdHocRunPersistsGeneratedBranch(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Status: "in_progress"}}
	runner := &recordingAgentRunner{}
	wt := &fakeWorktrees{info: orchestrator.WorktreeInfo{Path: "/wt/AGT-01", Branch: "agent/agt-01", Created: true}}

	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetAgentRunner(runner)
	svc.SetWorktrees(wt)

	if _, err := svc.StartAdHocRun(context.Background(), "AGT-01", "opencode", "go"); err != nil {
		t.Fatalf("StartAdHocRun: %v", err)
	}
	if fs.lastBranch != "agent/agt-01" {
		t.Errorf("SetBranch = %q, want agent/agt-01 persisted on ticket", fs.lastBranch)
	}
}

func TestStartAdHocRunKeepsExplicitBranch(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Status: "in_progress", Branch: "feature/custom"}}
	runner := &recordingAgentRunner{}
	wt := &fakeWorktrees{info: orchestrator.WorktreeInfo{Path: "/wt/AGT-01", Branch: "feature/custom", Created: true}}

	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetAgentRunner(runner)
	svc.SetWorktrees(wt)

	if _, err := svc.StartAdHocRun(context.Background(), "AGT-01", "opencode", "go"); err != nil {
		t.Fatalf("StartAdHocRun: %v", err)
	}
	if fs.lastBranch != "" {
		t.Errorf("SetBranch should not be called when ticket already has a branch, got %q", fs.lastBranch)
	}
}

func TestStartAdHocRunNoWorktreesConfigured(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Status: "in_progress"}}
	runner := &recordingAgentRunner{}

	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetAgentRunner(runner)

	if _, err := svc.StartAdHocRun(context.Background(), "AGT-01", "opencode", "do it"); err != nil {
		t.Fatalf("StartAdHocRun: %v", err)
	}
	if runner.lastReq.WorkDir != "" {
		t.Errorf("WorkDir = %q, want empty without worktrees", runner.lastReq.WorkDir)
	}
}

func TestStartAdHocRunEnsureFailureAbortsRun(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Status: "in_progress"}}
	runner := &recordingAgentRunner{}
	wt := &fakeWorktrees{ensureErr: errors.New("git exploded")}

	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetAgentRunner(runner)
	svc.SetWorktrees(wt)

	_, err := svc.StartAdHocRun(context.Background(), "AGT-01", "opencode", "do it")
	if err == nil {
		t.Fatal("StartAdHocRun should fail when Ensure fails")
	}
	if runner.lastReq.SessionID != "" {
		t.Error("runner should not start when Ensure fails")
	}
	if fs.lastAgentActive {
		t.Error("agent_active should be rolled back to false")
	}
}

func TestStartAdHocRunNoTicketSkipsWorktree(t *testing.T) {
	fs := &fakeStore{}
	runner := &recordingAgentRunner{}
	wt := &fakeWorktrees{info: orchestrator.WorktreeInfo{Path: "/wt/x", Created: true}}

	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetAgentRunner(runner)
	svc.SetWorktrees(wt)

	if _, err := svc.StartAdHocRun(context.Background(), "", "opencode", "do it"); err != nil {
		t.Fatalf("StartAdHocRun: %v", err)
	}
	if runner.lastReq.WorkDir != "" {
		t.Errorf("WorkDir = %q, want empty for ticketless run", runner.lastReq.WorkDir)
	}
	if wt.lastTicket.ID != "" {
		t.Error("Ensure should not be called for ticketless run")
	}
}

func TestStartApprovedRunEnsuresWorktree(t *testing.T) {
	fs := &fakeStore{
		ticket:   store.Ticket{ID: "AGT-01", Status: "in_progress"},
		proposal: store.Proposal{ID: "PRO-01", TicketID: "AGT-01", Agent: "opencode", Status: "approved", Prompt: "p"},
	}
	runner := &recordingAgentRunner{}
	wt := &fakeWorktrees{info: orchestrator.WorktreeInfo{Path: "/wt/AGT-01", Branch: "agent/agt-01", Created: true}}

	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetAgentRunner(runner)
	svc.SetWorktrees(wt)

	_, err := svc.StartApprovedRun(context.Background(), "PRO-01")
	if err != nil {
		t.Fatalf("StartApprovedRun: %v", err)
	}
	if runner.lastReq.WorkDir != "/wt/AGT-01" {
		t.Errorf("RunRequest.WorkDir = %q, want /wt/AGT-01", runner.lastReq.WorkDir)
	}
	if fs.lastWorktreePath != "/wt/AGT-01" {
		t.Errorf("SetWorktreePath = %q, want /wt/AGT-01", fs.lastWorktreePath)
	}
}

func TestEnsureNotCalledTwiceEventOnlyOnCreate(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Status: "in_progress", WorktreePath: "/wt/AGT-01"}}
	runner := &recordingAgentRunner{}
	wt := &fakeWorktrees{info: orchestrator.WorktreeInfo{Path: "/wt/AGT-01", Branch: "agent/agt-01", Created: false}}

	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetAgentRunner(runner)
	svc.SetWorktrees(wt)

	if _, err := svc.StartAdHocRun(context.Background(), "AGT-01", "opencode", "go"); err != nil {
		t.Fatalf("StartAdHocRun: %v", err)
	}
	if fs.hasEventKind("worktree.created") {
		t.Error("worktree.created event should not fire when reusing worktree")
	}
}
