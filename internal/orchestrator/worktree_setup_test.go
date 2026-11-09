package orchestrator_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayandexyz/agent-board/internal/git"
	"github.com/ayandexyz/agent-board/internal/orchestrator"
	"github.com/ayandexyz/agent-board/internal/store"
)

func freshWorktreeScript() *gitScript {
	return &gitScript{respond: func(c gitCall) (string, error) {
		switch {
		case c.args[0] == "worktree" && c.args[1] == "list":
			return "worktree /repo", nil
		case c.args[0] == "rev-parse" && c.args[1] == "--verify":
			return "", errors.New("exit status 1")
		}
		return "", nil
	}}
}

func newSetupManager(s *gitScript, repoRoot, worktreeRoot string, setup orchestrator.WorktreeSetup) *orchestrator.WorktreeManager {
	m := orchestrator.NewWorktreeManager(git.New(s.runner()), git.NewGH(s.runner()), repoRoot, worktreeRoot, "agent/", "main")
	m.SetSetup(setup)
	return m
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureCopiesFilesIntoNewWorktree(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ".env"), "API_KEY=secret")
	writeFile(t, filepath.Join(repo, "config", "dev.local.json"), `{"a":1}`)
	writeFile(t, filepath.Join(repo, "config", "prod.json"), `{}`)

	m := newSetupManager(freshWorktreeScript(), repo, t.TempDir(), orchestrator.WorktreeSetup{
		CopyFiles: []string{".env", "config/*.local.json", ".env.missing"},
	})

	info, err := m.Ensure(context.Background(), store.Ticket{ID: "AGT-01"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(info.Path, ".env"))
	if err != nil || string(got) != "API_KEY=secret" {
		t.Errorf(".env in worktree = %q, %v; want copied", got, err)
	}
	if _, err := os.Stat(filepath.Join(info.Path, "config", "dev.local.json")); err != nil {
		t.Errorf("glob match not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(info.Path, "config", "prod.json")); !os.IsNotExist(err) {
		t.Errorf("non-matching file copied: %v", err)
	}
}

func TestEnsureCopiesDirectories(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "certs", "dev", "key.pem"), "KEY")

	m := newSetupManager(freshWorktreeScript(), repo, t.TempDir(), orchestrator.WorktreeSetup{
		CopyFiles: []string{"certs"},
	})

	info, err := m.Ensure(context.Background(), store.Ticket{ID: "AGT-01"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(info.Path, "certs", "dev", "key.pem")); err != nil || string(got) != "KEY" {
		t.Errorf("nested file = %q, %v; want KEY", got, err)
	}
}

func TestEnsureRefusesCopyOutsideRepo(t *testing.T) {
	for _, pattern := range []string{"../secrets", "/etc/passwd"} {
		t.Run(pattern, func(t *testing.T) {
			m := newSetupManager(freshWorktreeScript(), t.TempDir(), t.TempDir(), orchestrator.WorktreeSetup{
				CopyFiles: []string{pattern},
			})
			if _, err := m.Ensure(context.Background(), store.Ticket{ID: "AGT-01"}); err == nil {
				t.Fatalf("Ensure with copy %q succeeded, want error", pattern)
			}
		})
	}
}

func TestEnsureRunsSetupScriptInNewWorktree(t *testing.T) {
	setup := &gitScript{}
	m := newSetupManager(freshWorktreeScript(), t.TempDir(), t.TempDir(), orchestrator.WorktreeSetup{
		Script: "pnpm install",
		Run:    setup.runner(),
	})

	info, err := m.Ensure(context.Background(), store.Ticket{ID: "AGT-01"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(setup.calls) != 1 {
		t.Fatalf("setup calls = %v, want one", setup.calls)
	}
	c := setup.calls[0]
	if c.dir != info.Path {
		t.Errorf("setup dir = %q, want worktree %q", c.dir, info.Path)
	}
	if c.name != "sh" || strings.Join(c.args, " ") != "-c pnpm install" {
		t.Errorf("setup command = %s %v, want sh -c pnpm install", c.name, c.args)
	}
}

func TestEnsureCopiesBeforeSetupRuns(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ".env"), "X=1")
	var envPresent bool
	run := func(ctx context.Context, dir, name string, args ...string) (string, error) {
		_, err := os.Stat(filepath.Join(dir, ".env"))
		envPresent = err == nil
		return "", nil
	}
	m := newSetupManager(freshWorktreeScript(), repo, t.TempDir(), orchestrator.WorktreeSetup{
		CopyFiles: []string{".env"},
		Script:    "make dev",
		Run:       run,
	})

	if _, err := m.Ensure(context.Background(), store.Ticket{ID: "AGT-01"}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !envPresent {
		t.Error("setup ran before .env was copied")
	}
}

func TestEnsureSkipsSetupForExistingWorktree(t *testing.T) {
	wtRoot := t.TempDir()
	path := filepath.Join(wtRoot, "AGT-01")
	s := &gitScript{respond: func(c gitCall) (string, error) {
		if c.args[0] == "worktree" && c.args[1] == "list" {
			return "worktree /repo\nworktree " + path, nil
		}
		return "", nil
	}}
	setup := &gitScript{}
	m := newSetupManager(s, t.TempDir(), wtRoot, orchestrator.WorktreeSetup{
		Script: "pnpm install",
		Run:    setup.runner(),
	})

	info, err := m.Ensure(context.Background(), store.Ticket{ID: "AGT-01"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if info.Created {
		t.Error("Created = true for a registered worktree")
	}
	if len(setup.calls) != 0 {
		t.Errorf("setup ran on a reused worktree: %v", setup.calls)
	}
}

func TestEnsureSetupFailureRemovesWorktree(t *testing.T) {
	s := freshWorktreeScript()
	run := func(ctx context.Context, dir, name string, args ...string) (string, error) {
		return "npm ERR! missing package.json", errors.New("exit status 1")
	}
	m := newSetupManager(s, t.TempDir(), t.TempDir(), orchestrator.WorktreeSetup{
		Script: "npm install",
		Run:    run,
	})

	_, err := m.Ensure(context.Background(), store.Ticket{ID: "AGT-01"})
	if err == nil {
		t.Fatal("Ensure succeeded, want setup error")
	}
	if !strings.Contains(err.Error(), "missing package.json") {
		t.Errorf("error %q does not carry the script output", err)
	}
	rm, ok := s.findCall("worktree", "remove")
	if !ok {
		t.Fatalf("worktree not removed after failed setup; calls %v", s.calls)
	}
	if !strings.Contains(strings.Join(rm.args, " "), "--force") {
		t.Errorf("worktree remove args = %v, want --force", rm.args)
	}
}
