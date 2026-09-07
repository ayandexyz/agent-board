package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type Runner func(ctx context.Context, dir, name string, args ...string) (string, error)

var (
	ErrConflict       = errors.New("git: merge/rebase conflict")
	ErrNotFastForward = errors.New("git: not a fast-forward")
	ErrDirty          = errors.New("git: working tree dirty")
)

func ExecRunner() Runner {
	return func(ctx context.Context, dir, name string, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		output := strings.TrimSpace(string(out))
		if err != nil {
			return output, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
		}
		return output, nil
	}
}

type Git struct {
	run Runner
}

func New(run Runner) *Git {
	return &Git{run: run}
}

func (g *Git) RepoRoot(ctx context.Context, dir string) (string, error) {
	out, err := g.run(ctx, dir, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("git.repoRoot: %w", err)
	}
	return out, nil
}

func (g *Git) CurrentBranch(ctx context.Context, dir string) (string, error) {
	out, err := g.run(ctx, dir, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git.currentBranch: %w", err)
	}
	return out, nil
}

func (g *Git) IsDirty(ctx context.Context, dir string) (bool, error) {
	out, err := g.run(ctx, dir, "git", "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("git.isDirty: %w", err)
	}
	return strings.TrimSpace(out) != "", nil
}

func (g *Git) BranchExists(ctx context.Context, root, branch string) (bool, error) {
	_, err := g.run(ctx, root, "git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil {
		return false, nil
	}
	return true, nil
}

func (g *Git) DetectBaseBranch(ctx context.Context, root string) (string, error) {
	for _, candidate := range []string{"main", "master"} {
		exists, err := g.BranchExists(ctx, root, candidate)
		if err != nil {
			return "", fmt.Errorf("git.detectBaseBranch: %w", err)
		}
		if exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("git.detectBaseBranch: neither main nor master exists")
}

func (g *Git) WorktreeAdd(ctx context.Context, root, path, branch, base string, createBranch bool) error {
	args := []string{"worktree", "add", path}
	if createBranch {
		args = append(args, "-b", branch, base)
	} else {
		args = append(args, branch)
	}
	if out, err := g.run(ctx, root, "git", args...); err != nil {
		return fmt.Errorf("git.worktreeAdd: %s: %w", out, err)
	}
	return nil
}

func (g *Git) WorktreeRemove(ctx context.Context, root, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	if out, err := g.run(ctx, root, "git", args...); err != nil {
		return fmt.Errorf("git.worktreeRemove: %s: %w", out, err)
	}
	return nil
}

func (g *Git) WorktreeList(ctx context.Context, root string) ([]string, error) {
	out, err := g.run(ctx, root, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git.worktreeList: %w", err)
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "worktree "); ok {
			paths = append(paths, rest)
		}
	}
	return paths, nil
}

func (g *Git) Rebase(ctx context.Context, worktreeDir, onto string) error {
	out, err := g.run(ctx, worktreeDir, "git", "rebase", onto)
	if err != nil {
		_, _ = g.run(ctx, worktreeDir, "git", "rebase", "--abort")
		return fmt.Errorf("git.rebase: %s: %w", out, ErrConflict)
	}
	return nil
}

func (g *Git) MergeFFOnly(ctx context.Context, root, branch string) error {
	out, err := g.run(ctx, root, "git", "merge", "--ff-only", branch)
	if err != nil {
		if strings.Contains(strings.ToLower(out), "fast-forward") {
			return fmt.Errorf("git.mergeFFOnly: %s: %w", out, ErrNotFastForward)
		}
		return fmt.Errorf("git.mergeFFOnly: %s: %w", out, err)
	}
	return nil
}

func (g *Git) Push(ctx context.Context, dir, remote, branch string) error {
	if out, err := g.run(ctx, dir, "git", "push", "-u", remote, branch); err != nil {
		return fmt.Errorf("git.push: %s: %w", out, err)
	}
	return nil
}

func (g *Git) DeleteBranch(ctx context.Context, root, branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	if out, err := g.run(ctx, root, "git", "branch", flag, branch); err != nil {
		return fmt.Errorf("git.deleteBranch: %s: %w", out, err)
	}
	return nil
}

func (g *Git) IsAncestor(ctx context.Context, root, ancestor, ref string) (bool, error) {
	if _, err := g.run(ctx, root, "git", "merge-base", "--is-ancestor", ancestor, ref); err != nil {
		return false, nil
	}
	return true, nil
}
