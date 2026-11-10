package orchestrator

import (
	"context"
	"fmt"
	"sync"

	"github.com/ayandexyz/agent-board/internal/store"
)

// RepoGit opens the git integration of a repository other than the board's
// own, for tickets whose Repo names it.
type RepoGit interface {
	For(ctx context.Context, root string) (Worktrees, error)
}

func (s *Service) SetRepoGit(r RepoGit) {
	s.repoGit = r
}

// ticketGit is the git integration that applies to one ticket. A nil field
// means that part is unavailable, as outside a git repository.
type ticketGit struct {
	worktrees Worktrees
	merger    Merger
	differ    Differ
}

func (s *Service) gitFor(ctx context.Context, t store.Ticket) (ticketGit, error) {
	if t.Repo == "" {
		return ticketGit{worktrees: s.worktrees, merger: s.merger, differ: s.differ}, nil
	}
	if s.repoGit == nil {
		return ticketGit{}, fmt.Errorf("orchestrator.gitFor %s: %w", t.Repo, ErrNoRepository)
	}
	w, err := s.repoGit.For(ctx, t.Repo)
	if err != nil {
		return ticketGit{}, fmt.Errorf("orchestrator.gitFor %s: %w", t.Repo, err)
	}
	g := ticketGit{worktrees: w}
	g.merger, _ = w.(Merger)
	g.differ, _ = w.(Differ)
	return g, nil
}

// RepoWorktreeManagers builds one WorktreeManager per repository on first use
// and keeps it, so the base branch is detected once per repository.
type RepoWorktreeManagers struct {
	New func(ctx context.Context, root string) (*WorktreeManager, error)

	mu     sync.Mutex
	byRoot map[string]*WorktreeManager
}

func (r *RepoWorktreeManagers) For(ctx context.Context, root string) (Worktrees, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.byRoot[root]; ok {
		return m, nil
	}
	m, err := r.New(ctx, root)
	if err != nil {
		return nil, err
	}
	if r.byRoot == nil {
		r.byRoot = make(map[string]*WorktreeManager)
	}
	r.byRoot[root] = m
	return m, nil
}
