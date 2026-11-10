package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayandexyz/agent-board/internal/workspace"
)

// gitAt answers like git for one repository rooted at root with the given
// origin remote, and like no repository anywhere else.
func gitAt(root, remote string) func(dir string, args ...string) (string, error) {
	return func(dir string, args ...string) (string, error) {
		if dir != root && !strings.HasPrefix(dir, root+string(filepath.Separator)) {
			return "", errors.New("not a git repository")
		}
		switch strings.Join(args, " ") {
		case "rev-parse --path-format=absolute --git-common-dir":
			return filepath.Join(root, ".git"), nil
		case "remote get-url origin":
			return remote, nil
		}
		return "", errors.New("exit status 1")
	}
}

func TestRepoBoardsOpensTheRepositorysOwnBoard(t *testing.T) {
	base := t.TempDir()
	root := t.TempDir()
	boards := repoBoards{baseDir: base, homeDB: filepath.Join(base, "home.db"), git: gitAt(root, "git@github.com:ayandexyz/freecode.git")}

	b, err := boards.Open(context.Background(), filepath.Join(root, "apps", "desktop"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Store.Close()

	if b.Root != root || b.Key != "ayandexyz-freecode" {
		t.Errorf("Board = %s key %s, want %s key ayandexyz-freecode", b.Root, b.Key, root)
	}
	if b.Store.TicketPrefix() != "FRE-" {
		t.Errorf("prefix = %q, want FRE-", b.Store.TicketPrefix())
	}
	if _, err := os.Stat(filepath.Join(base, "projects", "ayandexyz-freecode", "board.db")); err != nil {
		t.Errorf("board not opened at the repository's own path: %v", err)
	}
}

func TestRepoBoardsRefusesAPathOutsideGit(t *testing.T) {
	base := t.TempDir()
	boards := repoBoards{baseDir: base, homeDB: filepath.Join(base, "home.db"), git: gitAt("/nowhere", "")}

	_, err := boards.Open(context.Background(), t.TempDir())
	if !errors.Is(err, workspace.ErrNotARepo) {
		t.Errorf("err = %v, want ErrNotARepo", err)
	}
}

func TestRepoBoardsRefusesTheWorkspacesOwnDatabase(t *testing.T) {
	base := t.TempDir()
	root := t.TempDir()
	t.Setenv("AGENTBOARD_DB", filepath.Join(base, "shared.db"))
	boards := repoBoards{baseDir: base, homeDB: filepath.Join(base, "shared.db"), git: gitAt(root, "git@github.com:ayandexyz/freecode.git")}

	if _, err := boards.Open(context.Background(), root); !errors.Is(err, errSharedDatabase) {
		t.Errorf("err = %v, want errSharedDatabase", err)
	}
}

func TestRepoBoardsWorktreesUseTheRepositorysWorktreeRoot(t *testing.T) {
	base := t.TempDir()
	root := t.TempDir()
	boards := repoBoards{baseDir: base, homeDB: filepath.Join(base, "home.db"), git: gitAt(root, "git@github.com:ayandexyz/freecode.git")}

	wm, err := boards.Worktrees(context.Background(), root)
	if err != nil {
		t.Fatalf("Worktrees: %v", err)
	}
	if got, want := wm.Root(), filepath.Join(base, "worktrees", "ayandexyz-freecode"); got != want {
		t.Errorf("worktree root = %q, want %q", got, want)
	}
}
