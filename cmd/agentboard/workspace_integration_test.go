//go:build integration

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayandexyz/agent-board/internal/config"
	"github.com/ayandexyz/agent-board/internal/orchestrator"
	"github.com/ayandexyz/agent-board/internal/store"
	"github.com/ayandexyz/agent-board/internal/workspace"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T, name string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "init", "-b", "main")
	gitIn(t, repo, "config", "user.email", "t@t")
	gitIn(t, repo, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-m", "init")
	resolved, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

type startedRunner struct{ req orchestrator.RunRequest }

func (r *startedRunner) Start(_ context.Context, req orchestrator.RunRequest) (orchestrator.RunHandle, error) {
	r.req = req
	return orchestrator.RunHandle{Outcome: "running"}, nil
}
func (r *startedRunner) GetPaneID(string) (string, bool) { return "", false }
func (r *startedRunner) Kill(string) error               { return nil }
func (r *startedRunner) SendInput(string, string) error  { return nil }

func TestIntegrationWorkspaceRunsAndMergesATicketInAnotherRepository(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	freecode := newRepo(t, "freecode")

	home, err := store.Open(filepath.Join(base, "workspace.db"), []string{"backlog", "in_progress", "review", "done"}, "WS-")
	if err != nil {
		t.Fatal(err)
	}
	defer home.Close()

	boards := repoBoards{baseDir: base, homeDB: filepath.Join(base, "workspace.db"), git: config.ExecGit}
	ws := workspace.New(home, "", boards.Open)
	defer ws.Close()

	runner := &startedRunner{}
	svc := orchestrator.NewService(ws, nil, nil, nil)
	svc.SetAgentRunner(runner)
	svc.SetRepoGit(&orchestrator.RepoWorktreeManagers{New: boards.Worktrees})
	svc.SetMergeConfig(orchestrator.MergeStrategyLocal)

	sub := filepath.Join(freecode, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	tk, err := svc.CreateTicket(ctx, orchestrator.CreateTicketInput{Title: "add b", Repo: sub})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if tk.ID != "FRE-01" || tk.Repo != freecode {
		t.Fatalf("ticket = %s in %q, want FRE-01 in %s", tk.ID, tk.Repo, freecode)
	}
	if err := svc.MoveTicketStatus(ctx, tk.ID, "in_progress"); err != nil {
		t.Fatal(err)
	}

	sess, err := svc.StartAdHocRun(ctx, tk.ID, "claude", "add b")
	if err != nil {
		t.Fatalf("StartAdHocRun: %v", err)
	}
	wt := runner.req.WorkDir
	if !strings.HasPrefix(wt, filepath.Join(base, "worktrees")) || filepath.Base(wt) != "FRE-01" {
		t.Fatalf("agent started in %q, want a FRE-01 worktree under %s", wt, base)
	}
	if got := gitIn(t, wt, "rev-parse", "--abbrev-ref", "HEAD"); got != "agent/fre-01" {
		t.Fatalf("worktree branch = %q, want agent/fre-01", got)
	}
	if common := gitIn(t, wt, "rev-parse", "--path-format=absolute", "--git-common-dir"); common != filepath.Join(freecode, ".git") {
		t.Fatalf("worktree belongs to %q, want freecode's repository", common)
	}

	if err := os.WriteFile(filepath.Join(wt, "b.txt"), []byte("b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wt, "add", ".")
	gitIn(t, wt, "commit", "-m", "add b")
	if err := svc.StopSession(ctx, sess.ID); err != nil {
		t.Fatalf("StopSession: %v", err)
	}
	if err := svc.MoveTicketStatus(ctx, tk.ID, "review"); err != nil {
		t.Fatal(err)
	}

	res, err := svc.MergeTicket(ctx, tk.ID, "")
	if err != nil || res.Outcome != orchestrator.MergeOutcomeMerged {
		t.Fatalf("MergeTicket = %+v, %v", res, err)
	}
	if res.Base != "main" {
		t.Errorf("merged into %q, want main", res.Base)
	}
	if _, err := os.Stat(filepath.Join(freecode, "b.txt")); err != nil {
		t.Errorf("merge did not reach freecode's main checkout: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("worktree %s still exists after merge", wt)
	}

	own, err := boards.Open(ctx, freecode)
	if err != nil {
		t.Fatalf("open freecode's own board: %v", err)
	}
	defer own.Store.Close()
	direct, err := own.Store.GetTicket(ctx, "FRE-01")
	if err != nil {
		t.Fatalf("freecode's own board lacks FRE-01: %v", err)
	}
	if direct.Status != "done" {
		t.Errorf("freecode's own board shows %s, want done", direct.Status)
	}
}
