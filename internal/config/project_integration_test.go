//go:build integration

package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main", dir},
		{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

func TestResolveProjectSurvivesMoveAndWorktrees(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "notes")
	gitInit(t, repo)

	first := ResolveProject(ExecGit, repo)

	moved := filepath.Join(tmp, "elsewhere", "renamed")
	if err := os.MkdirAll(filepath.Dir(moved), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(repo, moved); err != nil {
		t.Fatal(err)
	}
	if got := ResolveProject(ExecGit, moved); got.Key != first.Key {
		t.Errorf("after move Key = %q, want %q", got.Key, first.Key)
	}

	wt := filepath.Join(tmp, "wt", "AGT-01")
	if out, err := exec.Command("git", "-C", moved, "worktree", "add", "-q", "-b", "agent/AGT-01", wt).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v: %s", err, out)
	}
	if got := ResolveProject(ExecGit, filepath.Join(wt)); got.Key != first.Key {
		t.Errorf("from worktree Key = %q, want %q", got.Key, first.Key)
	}

	sub := filepath.Join(moved, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if got := ResolveProject(ExecGit, sub); got.Key != first.Key {
		t.Errorf("from subdir Key = %q, want %q", got.Key, first.Key)
	}
}

func TestResolveProjectSeparatesSameNamedRepos(t *testing.T) {
	tmp := t.TempDir()
	a := filepath.Join(tmp, "work", "api")
	b := filepath.Join(tmp, "oss", "api")
	gitInit(t, a)
	gitInit(t, b)

	ka := ResolveProject(ExecGit, a).Key
	kb := ResolveProject(ExecGit, b).Key
	if ka == kb {
		t.Fatalf("two different repos named api share key %q", ka)
	}
}
