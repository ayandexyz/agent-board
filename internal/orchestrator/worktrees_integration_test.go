//go:build integration

package orchestrator_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ayan-de/agent-board/internal/git"
	"github.com/ayan-de/agent-board/internal/orchestrator"
	"github.com/ayan-de/agent-board/internal/store"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v: %s", args, err, out)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "git", "init", "-b", "main")
	gitRun(t, repo, "git", "config", "user.email", "t@t")
	gitRun(t, repo, "git", "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "git", "add", ".")
	gitRun(t, repo, "git", "commit", "-m", "init")
	return repo
}

func TestIntegrationWorktreeLifecycle(t *testing.T) {
	repo := initRepo(t)
	ctx := context.Background()
	g := git.New(git.ExecRunner())

	root, err := g.RepoRoot(ctx, repo)
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}

	wtRoot := filepath.Join(t.TempDir(), "worktrees")
	wm := orchestrator.NewWorktreeManager(g, git.NewGH(git.ExecRunner()), root, wtRoot, "agent/", "")

	ticket := store.Ticket{ID: "AGT-01"}
	info, err := wm.Ensure(ctx, ticket)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if info.Branch != "agent/agt-01" || !info.Created {
		t.Fatalf("info = %+v, want created agent/agt-01", info)
	}

	info2, err := wm.Ensure(ctx, ticket)
	if err != nil {
		t.Fatalf("re-Ensure: %v", err)
	}
	if info2.Created {
		t.Error("re-Ensure should reuse the worktree")
	}

	if err := os.WriteFile(filepath.Join(info.Path, "feature.txt"), []byte("done\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, info.Path, "git", "add", ".")
	gitRun(t, info.Path, "git", "commit", "-m", "agent work")

	ticket.WorktreePath = info.Path
	res, err := wm.MergeLocal(ctx, ticket)
	if err != nil {
		t.Fatalf("MergeLocal: %v", err)
	}
	if res.Outcome != orchestrator.MergeOutcomeMerged {
		t.Fatalf("Outcome = %s (%s), want merged", res.Outcome, res.Detail)
	}

	if err := wm.Cleanup(ctx, ticket, false); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "feature.txt")); err != nil {
		t.Error("merged file should exist on main")
	}
	if _, err := os.Stat(info.Path); !os.IsNotExist(err) {
		t.Error("worktree directory should be removed")
	}
	if exists, _ := g.BranchExists(ctx, root, "agent/agt-01"); exists {
		t.Error("branch should be deleted after merge")
	}
}

func TestIntegrationMergeConflict(t *testing.T) {
	repo := initRepo(t)
	ctx := context.Background()
	g := git.New(git.ExecRunner())
	root, _ := g.RepoRoot(ctx, repo)

	wtRoot := filepath.Join(t.TempDir(), "worktrees")
	wm := orchestrator.NewWorktreeManager(g, git.NewGH(git.ExecRunner()), root, wtRoot, "agent/", "")

	ticket := store.Ticket{ID: "AGT-02"}
	info, err := wm.Ensure(ctx, ticket)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	if err := os.WriteFile(filepath.Join(info.Path, "a.txt"), []byte("agent change\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, info.Path, "git", "add", ".")
	gitRun(t, info.Path, "git", "commit", "-m", "agent conflicting")

	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("main change\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "git", "add", ".")
	gitRun(t, root, "git", "commit", "-m", "main conflicting")

	ticket.WorktreePath = info.Path
	res, err := wm.MergeLocal(ctx, ticket)
	if err != nil {
		t.Fatalf("MergeLocal: %v", err)
	}
	if res.Outcome != orchestrator.MergeOutcomeConflict {
		t.Fatalf("Outcome = %s, want conflict", res.Outcome)
	}

	dirty, err := wm.IsDirty(ctx, info.Path)
	if err != nil {
		t.Fatalf("IsDirty: %v", err)
	}
	if dirty {
		t.Error("worktree should be clean after rebase abort")
	}
	if _, err := os.Stat(info.Path); err != nil {
		t.Error("worktree must survive a conflict")
	}
}

func TestIntegrationDirtyWorktreeRefusesRemove(t *testing.T) {
	repo := initRepo(t)
	ctx := context.Background()
	g := git.New(git.ExecRunner())
	root, _ := g.RepoRoot(ctx, repo)

	wtRoot := filepath.Join(t.TempDir(), "worktrees")
	wm := orchestrator.NewWorktreeManager(g, git.NewGH(git.ExecRunner()), root, wtRoot, "agent/", "")

	info, err := wm.Ensure(ctx, store.Ticket{ID: "AGT-03"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if err := os.WriteFile(filepath.Join(info.Path, "uncommitted.txt"), []byte("wip\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := wm.Remove(ctx, info.Path, false); err == nil {
		t.Fatal("Remove should refuse a dirty worktree")
	}
	if _, err := os.Stat(filepath.Join(info.Path, "uncommitted.txt")); err != nil {
		t.Error("uncommitted work must survive")
	}
}
