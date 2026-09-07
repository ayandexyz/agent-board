package orchestrator_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ayan-de/agent-board/internal/git"
	"github.com/ayan-de/agent-board/internal/orchestrator"
	"github.com/ayan-de/agent-board/internal/store"
)

type fakeGitOps struct {
	fakeWorktrees

	localResult orchestrator.MergeResult
	localErr    error
	prResult    orchestrator.MergeResult
	prErr       error
	prState     git.PRInfo
	prStateErr  error

	localCalled   bool
	prCalled      bool
	cleanupCalled bool
	cleanupForce  bool
	lastTitle     string
	lastBody      string
}

func (f *fakeGitOps) MergeLocal(_ context.Context, t store.Ticket) (orchestrator.MergeResult, error) {
	f.localCalled = true
	return f.localResult, f.localErr
}

func (f *fakeGitOps) CreatePR(_ context.Context, t store.Ticket, title, body string) (orchestrator.MergeResult, error) {
	f.prCalled = true
	f.lastTitle = title
	f.lastBody = body
	return f.prResult, f.prErr
}

func (f *fakeGitOps) PRState(_ context.Context, t store.Ticket) (git.PRInfo, error) {
	return f.prState, f.prStateErr
}

func (f *fakeGitOps) Cleanup(_ context.Context, t store.Ticket, forceBranch bool) error {
	f.cleanupCalled = true
	f.cleanupForce = forceBranch
	return nil
}

func newMergeService(fs *fakeStore, ops *fakeGitOps) *orchestrator.Service {
	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetWorktrees(ops)
	svc.SetMergeConfig(orchestrator.MergeStrategyLocal)
	return svc
}

func TestMergeTicketLocalHappyPath(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Status: "review", WorktreePath: "/wt/AGT-01", Branch: "agent/agt-01"}}
	ops := &fakeGitOps{localResult: orchestrator.MergeResult{Outcome: orchestrator.MergeOutcomeMerged, Branch: "agent/agt-01"}}
	svc := newMergeService(fs, ops)

	res, err := svc.MergeTicket(context.Background(), "AGT-01", "")
	if err != nil {
		t.Fatalf("MergeTicket: %v", err)
	}
	if res.Outcome != orchestrator.MergeOutcomeMerged {
		t.Errorf("Outcome = %q, want merged", res.Outcome)
	}
	if !ops.localCalled {
		t.Error("MergeLocal should be called")
	}
	if fs.lastMoveStatus != "done" {
		t.Errorf("MoveStatus = %q, want done", fs.lastMoveStatus)
	}
	if !ops.cleanupCalled {
		t.Error("Cleanup should be called after merge")
	}
	if fs.lastWorktreePath != "" {
		t.Errorf("worktree path should be cleared, got %q", fs.lastWorktreePath)
	}
	if !fs.hasEventKind("merge.started") || !fs.hasEventKind("merge.completed") {
		t.Errorf("expected merge.started + merge.completed events, got %v", fs.events)
	}
}

func TestMergeTicketLocalConflict(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Status: "review", WorktreePath: "/wt/AGT-01"}}
	ops := &fakeGitOps{localResult: orchestrator.MergeResult{Outcome: orchestrator.MergeOutcomeConflict, Detail: "CONFLICT in foo.go"}}
	svc := newMergeService(fs, ops)

	res, err := svc.MergeTicket(context.Background(), "AGT-01", "")
	if err != nil {
		t.Fatalf("MergeTicket conflict should not be a Go error: %v", err)
	}
	if res.Outcome != orchestrator.MergeOutcomeConflict {
		t.Errorf("Outcome = %q, want conflict", res.Outcome)
	}
	if fs.lastMoveStatus == "done" {
		t.Error("ticket must stay in review on conflict")
	}
	if ops.cleanupCalled {
		t.Error("Cleanup must not run on conflict")
	}
	if !fs.hasEventKind("merge.conflict") {
		t.Errorf("merge.conflict event missing, got %v", fs.events)
	}
}

func TestMergeTicketDirtyWorktree(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Status: "review", WorktreePath: "/wt/AGT-01"}}
	ops := &fakeGitOps{}
	ops.dirty = true
	svc := newMergeService(fs, ops)

	res, err := svc.MergeTicket(context.Background(), "AGT-01", "")
	if err != nil {
		t.Fatalf("MergeTicket: %v", err)
	}
	if res.Outcome != orchestrator.MergeOutcomeDirty {
		t.Errorf("Outcome = %q, want dirty", res.Outcome)
	}
	if ops.localCalled {
		t.Error("MergeLocal must not run on dirty worktree")
	}
	if fs.lastMoveStatus == "done" {
		t.Error("ticket must stay in review")
	}
}

func TestMergeTicketRefusesActiveSession(t *testing.T) {
	fs := &fakeStore{
		ticket:        store.Ticket{ID: "AGT-01", Status: "review", WorktreePath: "/wt/AGT-01"},
		activeSession: true,
	}
	ops := &fakeGitOps{}
	svc := newMergeService(fs, ops)

	_, err := svc.MergeTicket(context.Background(), "AGT-01", "")
	if err == nil {
		t.Fatal("MergeTicket should refuse while a session is active")
	}
	if ops.localCalled || ops.prCalled {
		t.Error("no git operations should run with an active session")
	}
}

func TestMergeTicketPRStrategy(t *testing.T) {
	fs := &fakeStore{
		ticket:       store.Ticket{ID: "AGT-01", Title: "Add feature", Prompt: "the prompt", Status: "review", WorktreePath: "/wt/AGT-01"},
		contextCarry: store.ContextCarry{Summary: "carry summary"},
	}
	ops := &fakeGitOps{prResult: orchestrator.MergeResult{
		Outcome: orchestrator.MergeOutcomePRCreated,
		Branch:  "agent/agt-01",
		PRURL:   "https://github.com/u/r/pull/7",
	}}
	svc := newMergeService(fs, ops)

	res, err := svc.MergeTicket(context.Background(), "AGT-01", orchestrator.MergeStrategyPR)
	if err != nil {
		t.Fatalf("MergeTicket: %v", err)
	}
	if res.Outcome != orchestrator.MergeOutcomePRCreated {
		t.Errorf("Outcome = %q, want pr_created", res.Outcome)
	}
	if !ops.prCalled {
		t.Error("CreatePR should be called")
	}
	if ops.lastTitle != "AGT-01: Add feature" {
		t.Errorf("PR title = %q, unexpected", ops.lastTitle)
	}
	if fs.lastPRURL != "https://github.com/u/r/pull/7" {
		t.Errorf("SetPRURL = %q, unexpected", fs.lastPRURL)
	}
	if fs.lastMoveStatus == "done" {
		t.Error("ticket must stay in review after PR creation")
	}
	if ops.cleanupCalled {
		t.Error("Cleanup must not run after PR creation")
	}
	if !fs.hasEventKind("pr.created") {
		t.Errorf("pr.created event missing, got %v", fs.events)
	}
}

func TestMoveTicketStatusPlainTransition(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Status: "backlog"}}
	svc := newMergeService(fs, &fakeGitOps{})

	if err := svc.MoveTicketStatus(context.Background(), "AGT-01", "in_progress"); err != nil {
		t.Fatalf("MoveTicketStatus: %v", err)
	}
	if fs.lastMoveStatus != "in_progress" {
		t.Errorf("MoveStatus = %q, want in_progress", fs.lastMoveStatus)
	}
	if !fs.hasEventKind("status.changed") {
		t.Errorf("status.changed event missing, got %v", fs.events)
	}
}

func TestMoveTicketStatusReviewToDoneRequiresMerge(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Status: "review", WorktreePath: "/wt/AGT-01", Branch: "agent/agt-01"}}
	svc := newMergeService(fs, &fakeGitOps{})

	err := svc.MoveTicketStatus(context.Background(), "AGT-01", "done")
	var mergeReq *orchestrator.MergeRequiredError
	if !errors.As(err, &mergeReq) {
		t.Fatalf("err = %v, want *MergeRequiredError", err)
	}
	if mergeReq.TicketID != "AGT-01" {
		t.Errorf("TicketID = %q, want AGT-01", mergeReq.TicketID)
	}
	if mergeReq.Strategy != orchestrator.MergeStrategyLocal {
		t.Errorf("Strategy = %q, want local default", mergeReq.Strategy)
	}
	if fs.lastMoveStatus == "done" {
		t.Error("status must not move to done without merge")
	}
}

func TestMoveTicketStatusReviewToDonePRMerged(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Status: "review", WorktreePath: "/wt/AGT-01", PRURL: "https://pr/7"}}
	ops := &fakeGitOps{prState: git.PRInfo{State: "MERGED", URL: "https://pr/7"}}
	svc := newMergeService(fs, ops)

	if err := svc.MoveTicketStatus(context.Background(), "AGT-01", "done"); err != nil {
		t.Fatalf("MoveTicketStatus with merged PR: %v", err)
	}
	if fs.lastMoveStatus != "done" {
		t.Errorf("MoveStatus = %q, want done", fs.lastMoveStatus)
	}
	if !ops.cleanupCalled {
		t.Error("Cleanup should run when PR is merged")
	}
	if !ops.cleanupForce {
		t.Error("branch delete should be forced after external merge (squash-safe)")
	}
}

func TestMoveTicketStatusReviewToDonePROpen(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Status: "review", WorktreePath: "/wt/AGT-01", PRURL: "https://pr/7"}}
	ops := &fakeGitOps{prState: git.PRInfo{State: "OPEN", URL: "https://pr/7"}}
	svc := newMergeService(fs, ops)

	err := svc.MoveTicketStatus(context.Background(), "AGT-01", "done")
	var mergeReq *orchestrator.MergeRequiredError
	if !errors.As(err, &mergeReq) {
		t.Fatalf("err = %v, want *MergeRequiredError", err)
	}
	if mergeReq.PRURL != "https://pr/7" {
		t.Errorf("PRURL = %q, want the open PR url", mergeReq.PRURL)
	}
	if fs.lastMoveStatus == "done" {
		t.Error("status must not move while PR is open")
	}
}

func TestMoveTicketStatusReviewToDoneNoWorktreePassesThrough(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Status: "review"}}
	svc := newMergeService(fs, &fakeGitOps{})

	if err := svc.MoveTicketStatus(context.Background(), "AGT-01", "done"); err != nil {
		t.Fatalf("MoveTicketStatus: %v", err)
	}
	if fs.lastMoveStatus != "done" {
		t.Errorf("MoveStatus = %q, want done for ticket without worktree", fs.lastMoveStatus)
	}
}

func TestDeleteTicketRefusesDirtyWorktree(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Status: "backlog", WorktreePath: "/wt/AGT-01"}}
	ops := &fakeGitOps{}
	ops.removeErr = errors.New("dirty: " + git.ErrDirty.Error())
	svc := newMergeService(fs, ops)

	if err := svc.DeleteTicket(context.Background(), "AGT-01"); err == nil {
		t.Fatal("DeleteTicket should fail when worktree removal fails")
	}
	if fs.deletedTicketID != "" {
		t.Error("ticket must not be deleted when worktree is dirty")
	}
}

func TestDeleteTicketCleansUpWorktree(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Status: "backlog", WorktreePath: "/wt/AGT-01"}}
	ops := &fakeGitOps{}
	svc := newMergeService(fs, ops)

	if err := svc.DeleteTicket(context.Background(), "AGT-01"); err != nil {
		t.Fatalf("DeleteTicket: %v", err)
	}
	if len(ops.removed) != 1 || ops.removed[0] != "/wt/AGT-01" {
		t.Errorf("removed = %v, want [/wt/AGT-01]", ops.removed)
	}
	if fs.deletedTicketID != "AGT-01" {
		t.Errorf("deleted ticket = %q, want AGT-01", fs.deletedTicketID)
	}
}

func TestDeleteTicketWithoutWorktree(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Status: "backlog"}}
	ops := &fakeGitOps{}
	svc := newMergeService(fs, ops)

	if err := svc.DeleteTicket(context.Background(), "AGT-01"); err != nil {
		t.Fatalf("DeleteTicket: %v", err)
	}
	if len(ops.removed) != 0 {
		t.Errorf("no worktree removal expected, got %v", ops.removed)
	}
	if fs.deletedTicketID != "AGT-01" {
		t.Errorf("deleted ticket = %q, want AGT-01", fs.deletedTicketID)
	}
}
