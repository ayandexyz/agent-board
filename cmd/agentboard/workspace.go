package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/ayandexyz/agent-board/internal/config"
	"github.com/ayandexyz/agent-board/internal/git"
	"github.com/ayandexyz/agent-board/internal/orchestrator"
	"github.com/ayandexyz/agent-board/internal/store"
	"github.com/ayandexyz/agent-board/internal/workspace"
)

var errSharedDatabase = errors.New("repository resolves to this board's own database; unset AGENTBOARD_DB")

// repoBoards opens another repository's board and git integration the way
// running agentboard inside that repository would, from its own config.
type repoBoards struct {
	baseDir string
	homeDB  string
	git     config.GitRunner
}

func (r repoBoards) load(path string) (*config.Config, string, error) {
	root, ok := config.RepoRoot(r.git, path)
	if !ok {
		return nil, "", fmt.Errorf("%s: %w", path, workspace.ErrNotARepo)
	}
	cfg, err := config.LoadFromDir(r.baseDir, config.ResolveProject(r.git, root))
	if err != nil {
		return nil, "", err
	}
	return cfg, root, nil
}

func (r repoBoards) Open(_ context.Context, path string) (workspace.Board, error) {
	cfg, root, err := r.load(path)
	if err != nil {
		return workspace.Board{}, err
	}
	if cfg.DB.Path == r.homeDB {
		return workspace.Board{}, fmt.Errorf("%s: %w", root, errSharedDatabase)
	}
	statuses := make([]string, len(cfg.Board.Columns))
	for i, col := range cfg.Board.Columns {
		statuses[i] = col.Status
	}
	s, err := store.Open(cfg.DB.Path, statuses, cfg.Board.Prefix)
	if err != nil {
		return workspace.Board{}, err
	}
	return workspace.Board{Root: root, Key: cfg.ProjectKey, Store: s}, nil
}

func (r repoBoards) Worktrees(_ context.Context, root string) (*orchestrator.WorktreeManager, error) {
	cfg, root, err := r.load(root)
	if err != nil {
		return nil, err
	}
	wm := orchestrator.NewWorktreeManager(
		git.New(git.ExecRunner()),
		git.NewGH(git.ExecRunner()),
		root,
		cfg.Git.ResolvedWorktreeRoot(r.baseDir, cfg.ProjectKey),
		cfg.Git.BranchPrefix,
		cfg.Git.BaseBranch,
	)
	wm.SetSetup(orchestrator.WorktreeSetup{
		CopyFiles: cfg.Git.CopyFiles,
		Script:    cfg.Git.SetupScript,
		Run:       git.ExecRunner(),
	})
	return wm, nil
}
