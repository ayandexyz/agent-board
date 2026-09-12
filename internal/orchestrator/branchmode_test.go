package orchestrator_test

import (
	"context"
	"testing"

	"github.com/ayan-de/agent-board/internal/orchestrator"
	"github.com/ayan-de/agent-board/internal/store"
)

func TestStartAdHocRunSkipsWorktreeForCurrentBranch(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{
		ID: "AGT-01", Title: "T", Status: "in_progress", BranchMode: store.BranchModeCurrent,
	}}
	runner := &recordingAgentRunner{}
	wt := &fakeWorktrees{info: orchestrator.WorktreeInfo{Path: "/wt/AGT-01", Branch: "agent/agt-01", Created: true}}

	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetAgentRunner(runner)
	svc.SetWorktrees(wt)

	if _, err := svc.StartAdHocRun(context.Background(), "AGT-01", "opencode", "go"); err != nil {
		t.Fatalf("StartAdHocRun: %v", err)
	}

	if runner.lastReq.WorkDir != "" {
		t.Errorf("RunRequest.WorkDir = %q, want empty (repo root)", runner.lastReq.WorkDir)
	}
	if wt.lastTicket.ID != "" {
		t.Errorf("Ensure called for current-branch ticket: %+v", wt.lastTicket)
	}
	if fs.lastWorktreePath != "" {
		t.Errorf("SetWorktreePath = %q, want no worktree recorded", fs.lastWorktreePath)
	}
}

func TestCompleteTicketSkipsMergeGateForCurrentBranch(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{
		ID: "AGT-01", Status: "review", Branch: "feat/x", BranchMode: store.BranchModeCurrent,
	}}
	svc := newMergeService(fs, &fakeGitOps{})

	if err := svc.CompleteTicket(context.Background(), "AGT-01"); err != nil {
		t.Fatalf("CompleteTicket: %v", err)
	}
	if fs.lastMoveStatus != "done" {
		t.Errorf("lastMoveStatus = %q, want done", fs.lastMoveStatus)
	}
}
