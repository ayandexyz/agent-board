package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ayandexyz/agent-board/internal/git"
	"github.com/ayandexyz/agent-board/internal/store"
)

type WorktreeManager struct {
	git          *git.Git
	gh           *git.GH
	repoRoot     string
	worktreeRoot string
	branchPrefix string

	mu         sync.Mutex
	baseBranch string

	setup WorktreeSetup
}

func NewWorktreeManager(g *git.Git, gh *git.GH, repoRoot, worktreeRoot, branchPrefix, baseBranch string) *WorktreeManager {
	return &WorktreeManager{
		git:          g,
		gh:           gh,
		repoRoot:     repoRoot,
		worktreeRoot: worktreeRoot,
		branchPrefix: branchPrefix,
		baseBranch:   baseBranch,
	}
}

func (m *WorktreeManager) base(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.baseBranch != "" {
		return m.baseBranch, nil
	}
	detected, err := m.git.DetectBaseBranch(ctx, m.repoRoot)
	if err != nil {
		return "", err
	}
	m.baseBranch = detected
	return detected, nil
}

func (m *WorktreeManager) branchFor(t store.Ticket) string {
	if t.Branch != "" {
		return t.Branch
	}
	return DefaultBranchName(m.branchPrefix, t.ID)
}

func (m *WorktreeManager) pathFor(t store.Ticket) string {
	return filepath.Join(m.worktreeRoot, t.ID)
}

func (m *WorktreeManager) Ensure(ctx context.Context, t store.Ticket) (WorktreeInfo, error) {
	path := m.pathFor(t)
	branch := m.branchFor(t)
	recorded := t.WorktreePath

	base, err := m.base(ctx)
	if err != nil {
		return WorktreeInfo{}, fmt.Errorf("orchestrator.ensureWorktree: %w", err)
	}

	registered, err := m.git.WorktreeList(ctx, m.repoRoot)
	if err != nil {
		return WorktreeInfo{}, fmt.Errorf("orchestrator.ensureWorktree: %w", err)
	}
	for _, p := range registered {
		if p == recorded || p == path {
			return WorktreeInfo{Path: p, Branch: branch, Base: base, Created: false}, nil
		}
	}

	branchExists, err := m.git.BranchExists(ctx, m.repoRoot, branch)
	if err != nil {
		return WorktreeInfo{}, fmt.Errorf("orchestrator.ensureWorktree: %w", err)
	}

	if err := os.MkdirAll(m.worktreeRoot, 0755); err != nil {
		return WorktreeInfo{}, fmt.Errorf("orchestrator.ensureWorktree: %w", err)
	}

	if err := m.git.WorktreeAdd(ctx, m.repoRoot, path, branch, base, !branchExists); err != nil {
		return WorktreeInfo{}, fmt.Errorf("orchestrator.ensureWorktree: %w", err)
	}

	if err := m.prepare(ctx, path); err != nil {
		_ = m.git.WorktreeRemove(context.WithoutCancel(ctx), m.repoRoot, path, true)
		return WorktreeInfo{}, fmt.Errorf("orchestrator.ensureWorktree: %w", err)
	}

	return WorktreeInfo{Path: path, Branch: branch, Base: base, Created: true}, nil
}

func (m *WorktreeManager) Remove(ctx context.Context, path string, force bool) error {
	if !force {
		dirty, err := m.git.IsDirty(ctx, path)
		if err != nil {
			return fmt.Errorf("orchestrator.removeWorktree: %w", err)
		}
		if dirty {
			return fmt.Errorf("orchestrator.removeWorktree %s: %w", path, git.ErrDirty)
		}
	}
	if err := m.git.WorktreeRemove(ctx, m.repoRoot, path, force); err != nil {
		return fmt.Errorf("orchestrator.removeWorktree: %w", err)
	}
	return nil
}

func (m *WorktreeManager) IsDirty(ctx context.Context, path string) (bool, error) {
	return m.git.IsDirty(ctx, path)
}

func (m *WorktreeManager) worktreePath(t store.Ticket) string {
	if t.WorktreePath != "" {
		return t.WorktreePath
	}
	return m.pathFor(t)
}

func (m *WorktreeManager) MergeLocal(ctx context.Context, t store.Ticket) (MergeResult, error) {
	branch := m.branchFor(t)
	base, err := m.base(ctx)
	if err != nil {
		return MergeResult{}, fmt.Errorf("orchestrator.mergeLocal: %w", err)
	}

	rootDirty, err := m.git.IsDirty(ctx, m.repoRoot)
	if err != nil {
		return MergeResult{}, fmt.Errorf("orchestrator.mergeLocal: %w", err)
	}
	if rootDirty {
		return MergeResult{Outcome: MergeOutcomeDirty, Branch: branch,
			Detail: "repository root has uncommitted changes; commit or stash them first"}, nil
	}

	current, err := m.git.CurrentBranch(ctx, m.repoRoot)
	if err != nil {
		return MergeResult{}, fmt.Errorf("orchestrator.mergeLocal: %w", err)
	}
	if current != base {
		return MergeResult{Outcome: MergeOutcomeDirty, Branch: branch,
			Detail: fmt.Sprintf("repository root is on %q, check out %q before merging", current, base)}, nil
	}

	if err := m.git.Rebase(ctx, m.worktreePath(t), base); err != nil {
		if errors.Is(err, git.ErrConflict) {
			return MergeResult{Outcome: MergeOutcomeConflict, Branch: branch, Detail: err.Error()}, nil
		}
		return MergeResult{}, fmt.Errorf("orchestrator.mergeLocal: %w", err)
	}

	if err := m.git.MergeFFOnly(ctx, m.repoRoot, branch); err != nil {
		if errors.Is(err, git.ErrNotFastForward) {
			return MergeResult{Outcome: MergeOutcomeConflict, Branch: branch, Detail: err.Error()}, nil
		}
		return MergeResult{}, fmt.Errorf("orchestrator.mergeLocal: %w", err)
	}

	return MergeResult{Outcome: MergeOutcomeMerged, Branch: branch}, nil
}

func (m *WorktreeManager) CreatePR(ctx context.Context, t store.Ticket, title, body string) (MergeResult, error) {
	branch := m.branchFor(t)
	base, err := m.base(ctx)
	if err != nil {
		return MergeResult{}, fmt.Errorf("orchestrator.createPR: %w", err)
	}

	dir := m.worktreePath(t)
	if err := m.git.Push(ctx, dir, "origin", branch); err != nil {
		return MergeResult{}, fmt.Errorf("orchestrator.createPR: %w", err)
	}

	url, err := m.gh.PRCreate(ctx, dir, base, branch, title, body)
	if err != nil {
		return MergeResult{}, fmt.Errorf("orchestrator.createPR: %w", err)
	}

	return MergeResult{Outcome: MergeOutcomePRCreated, Branch: branch, PRURL: url}, nil
}

func (m *WorktreeManager) PRState(ctx context.Context, t store.Ticket) (git.PRInfo, error) {
	return m.gh.PRView(ctx, m.repoRoot, m.branchFor(t))
}

func (m *WorktreeManager) Cleanup(ctx context.Context, t store.Ticket, forceBranch bool) error {
	if err := m.Remove(ctx, m.worktreePath(t), false); err != nil {
		return err
	}
	if err := m.git.DeleteBranch(ctx, m.repoRoot, m.branchFor(t), forceBranch); err != nil {
		return fmt.Errorf("orchestrator.cleanup: %w", err)
	}
	return nil
}
