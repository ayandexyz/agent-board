package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ayandexyz/agent-board/internal/orchestrator"
	"github.com/ayandexyz/agent-board/internal/store"
)

// repoGit hands out one fake per repository root and records which roots
// were asked for.
type repoGit struct {
	byRoot map[string]orchestrator.Worktrees
	asked  []string
}

func (r *repoGit) For(_ context.Context, root string) (orchestrator.Worktrees, error) {
	r.asked = append(r.asked, root)
	w, ok := r.byRoot[root]
	if !ok {
		return nil, orchestrator.ErrNoRepository
	}
	return w, nil
}

func TestForeignTicketGetsItsWorktreeFromItsOwnRepository(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "FRE-01", Status: "in_progress", Repo: "/src/freecode"}}
	runner := &recordingAgentRunner{}
	home := &fakeWorktrees{info: orchestrator.WorktreeInfo{Path: "/wt/home/FRE-01"}}
	freecode := &fakeWorktrees{info: orchestrator.WorktreeInfo{Path: "/wt/freecode/FRE-01", Branch: "agent/fre-01", Created: true}}
	repos := &repoGit{byRoot: map[string]orchestrator.Worktrees{"/src/freecode": freecode}}

	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetAgentRunner(runner)
	svc.SetWorktrees(home)
	svc.SetRepoGit(repos)

	if _, err := svc.StartAdHocRun(context.Background(), "FRE-01", "claude", "go"); err != nil {
		t.Fatalf("StartAdHocRun: %v", err)
	}
	if runner.lastReq.WorkDir != "/wt/freecode/FRE-01" {
		t.Errorf("WorkDir = %q, want freecode's worktree", runner.lastReq.WorkDir)
	}
	if home.lastTicket.ID != "" {
		t.Error("the home repository was asked for a worktree of a freecode ticket")
	}
}

func TestForeignTicketOnCurrentBranchRunsInItsRepositoryRoot(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "FRE-01", Status: "in_progress", Repo: "/src/freecode", BranchMode: store.BranchModeCurrent}}
	runner := &recordingAgentRunner{}
	repos := &repoGit{byRoot: map[string]orchestrator.Worktrees{"/src/freecode": &fakeWorktrees{}}}

	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetAgentRunner(runner)
	svc.SetRepoGit(repos)

	if _, err := svc.StartAdHocRun(context.Background(), "FRE-01", "claude", "go"); err != nil {
		t.Fatalf("StartAdHocRun: %v", err)
	}
	if runner.lastReq.WorkDir != "/src/freecode" {
		t.Errorf("WorkDir = %q, want the repository root /src/freecode", runner.lastReq.WorkDir)
	}
}

func TestForeignTicketWhoseRepositoryCannotBeOpenedDoesNotStart(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "FRE-01", Status: "in_progress", Repo: "/src/gone"}}
	runner := &recordingAgentRunner{}

	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetAgentRunner(runner)
	svc.SetRepoGit(&repoGit{byRoot: map[string]orchestrator.Worktrees{}})

	_, err := svc.StartAdHocRun(context.Background(), "FRE-01", "claude", "go")
	if !errors.Is(err, orchestrator.ErrNoRepository) {
		t.Errorf("err = %v, want ErrNoRepository", err)
	}
	if runner.lastReq.SessionID != "" {
		t.Error("an agent was started in the wrong directory")
	}
}

func TestMergeForeignTicketUsesItsRepositorysMerger(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "FRE-01", Status: "review", WorktreePath: "/wt/FRE-01", Branch: "agent/fre-01", Repo: "/src/freecode"}}
	home := &fakeGitOps{}
	freecode := &fakeGitOps{localResult: orchestrator.MergeResult{Outcome: orchestrator.MergeOutcomeMerged}}

	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetWorktrees(home)
	svc.SetRepoGit(&repoGit{byRoot: map[string]orchestrator.Worktrees{"/src/freecode": freecode}})
	svc.SetMergeConfig(orchestrator.MergeStrategyLocal)

	res, err := svc.MergeTicket(context.Background(), "FRE-01", "")
	if err != nil {
		t.Fatalf("MergeTicket: %v", err)
	}
	if res.Outcome != orchestrator.MergeOutcomeMerged {
		t.Errorf("Outcome = %q, want merged", res.Outcome)
	}
	if !freecode.localCalled || !freecode.cleanupCalled {
		t.Error("freecode's merger did not merge and clean up")
	}
	if home.localCalled || home.cleanupCalled {
		t.Error("the home repository merged a freecode ticket")
	}
}

func TestMergeWorksForForeignTicketWhenTheWorkspaceIsNotAGitRepository(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "FRE-01", Status: "review", WorktreePath: "/wt/FRE-01", Repo: "/src/freecode"}}
	freecode := &fakeGitOps{localResult: orchestrator.MergeResult{Outcome: orchestrator.MergeOutcomeMerged}}

	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetRepoGit(&repoGit{byRoot: map[string]orchestrator.Worktrees{"/src/freecode": freecode}})

	if _, err := svc.MergeTicket(context.Background(), "FRE-01", ""); err != nil {
		t.Fatalf("MergeTicket: %v", err)
	}
	if !freecode.localCalled {
		t.Error("freecode's merger was not used")
	}
}

func TestCompleteForeignTicketAsksItsRepositoryAboutUnmergedWork(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "FRE-01", Status: "review", WorktreePath: "/wt/FRE-01", Branch: "agent/fre-01", Repo: "/src/freecode"}}
	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetRepoGit(&repoGit{byRoot: map[string]orchestrator.Worktrees{"/src/freecode": &fakeGitOps{}}})

	err := svc.CompleteTicket(context.Background(), "FRE-01")
	var mr *orchestrator.MergeRequiredError
	if !errors.As(err, &mr) {
		t.Errorf("err = %v, want MergeRequiredError for the unmerged freecode branch", err)
	}
}

func TestDeleteForeignTicketRemovesTheWorktreeInItsRepository(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "FRE-01", Status: "backlog", WorktreePath: "/wt/FRE-01", Repo: "/src/freecode"}}
	home := &fakeWorktrees{}
	freecode := &fakeWorktrees{}

	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetWorktrees(home)
	svc.SetRepoGit(&repoGit{byRoot: map[string]orchestrator.Worktrees{"/src/freecode": freecode}})

	if err := svc.DeleteTicket(context.Background(), "FRE-01"); err != nil {
		t.Fatalf("DeleteTicket: %v", err)
	}
	if len(freecode.removed) != 1 || len(home.removed) != 0 {
		t.Errorf("removed from freecode %v, home %v; want only freecode", freecode.removed, home.removed)
	}
}

func TestDiffForeignTicketUsesItsRepository(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "FRE-01", WorktreePath: "/wt/FRE-01", Repo: "/src/freecode"}}
	freecode := &diffingWorktrees{diff: orchestrator.TicketDiff{Patch: "freecode"}}

	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetRepoGit(&repoGit{byRoot: map[string]orchestrator.Worktrees{"/src/freecode": freecode}})

	got, err := svc.TicketDiff(context.Background(), "FRE-01")
	if err != nil {
		t.Fatalf("TicketDiff: %v", err)
	}
	if got.Patch != "freecode" {
		t.Errorf("Patch = %q, want freecode's diff", got.Patch)
	}
}

// basedGitOps is a merger that knows its repository's base branch.
type basedGitOps struct {
	fakeGitOps
	base string
}

func (b *basedGitOps) BaseBranch(context.Context) (string, error) { return b.base, nil }

func TestMergeRequiredNamesTheTargetBranchAndRepository(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "FRE-02", Status: "review", WorktreePath: "/wt/FRE-02", Branch: "agent/fre-02", Repo: "/src/freecode"}}
	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetRepoGit(&repoGit{byRoot: map[string]orchestrator.Worktrees{"/src/freecode": &basedGitOps{base: "develop"}}})

	err := svc.CompleteTicket(context.Background(), "FRE-02")
	var mr *orchestrator.MergeRequiredError
	if !errors.As(err, &mr) {
		t.Fatalf("err = %v, want MergeRequiredError", err)
	}
	if mr.Base != "develop" || mr.Repo != "/src/freecode" || mr.Branch != "agent/fre-02" {
		t.Errorf("MergeRequiredError = %+v, want agent/fre-02 into develop of /src/freecode", mr)
	}
}

func TestMergeRequiredNamesTheWorktree(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "FRE-02", Status: "review", WorktreePath: "/wt/FRE-02", Branch: "agent/fre-02", Repo: "/src/freecode"}}
	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetRepoGit(&repoGit{byRoot: map[string]orchestrator.Worktrees{"/src/freecode": &basedGitOps{base: "main"}}})

	var mr *orchestrator.MergeRequiredError
	if err := svc.CompleteTicket(context.Background(), "FRE-02"); !errors.As(err, &mr) {
		t.Fatalf("err = %v, want MergeRequiredError", err)
	}
	if mr.Worktree != "/wt/FRE-02" {
		t.Errorf("Worktree = %q, want /wt/FRE-02", mr.Worktree)
	}
}

func TestDirtyMergeSaysWhichWorktreeToCommitIn(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "FRE-02", Status: "review", WorktreePath: "/wt/FRE-02", Repo: "/src/freecode"}}
	ops := &fakeGitOps{}
	ops.dirty = true
	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetRepoGit(&repoGit{byRoot: map[string]orchestrator.Worktrees{"/src/freecode": ops}})

	res, err := svc.MergeTicket(context.Background(), "FRE-02", "")
	if err != nil {
		t.Fatalf("MergeTicket: %v", err)
	}
	if res.Outcome != orchestrator.MergeOutcomeDirty || !strings.Contains(res.Detail, "/wt/FRE-02") {
		t.Errorf("result = %+v, want a dirty outcome naming /wt/FRE-02", res)
	}
}
