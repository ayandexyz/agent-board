package orchestrator_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayan-de/agent-board/internal/git"
	"github.com/ayan-de/agent-board/internal/store"
)

func diffScript(numstat, patch, untracked string) *gitScript {
	return &gitScript{respond: func(c gitCall) (string, error) {
		switch c.args[0] {
		case "merge-base":
			return "base123", nil
		case "ls-files":
			return untracked, nil
		case "diff":
			if c.args[1] == "--numstat" {
				return numstat, nil
			}
			return patch, nil
		}
		return "", nil
	}}
}

func TestDiffWorktreeTicketComparesAgainstForkPoint(t *testing.T) {
	wt := t.TempDir()
	writeFile(t, filepath.Join(wt, "new.txt"), "a\nb\n")
	s := diffScript("2\t1\tmain.go", "diff --git a/main.go b/main.go", "new.txt")
	m := newTestManager(s, t.TempDir())

	d, err := m.Diff(context.Background(), store.Ticket{ID: "AGT-01", WorktreePath: wt})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	mb, ok := s.findCall("merge-base")
	if !ok || mb.dir != wt || strings.Join(mb.args, " ") != "merge-base main HEAD" {
		t.Errorf("merge-base call = %+v, want merge-base main HEAD in %s", mb, wt)
	}
	if d.Base != "base123" {
		t.Errorf("Base = %q, want base123", d.Base)
	}
	want := []git.FileChange{
		{Path: "main.go", Added: 2, Deleted: 1},
		{Path: "new.txt", Added: 2, Untracked: true},
	}
	if len(d.Files) != len(want) || d.Files[0] != want[0] || d.Files[1] != want[1] {
		t.Errorf("Files = %+v, want %+v", d.Files, want)
	}
	for _, s := range []string{"diff --git a/main.go b/main.go", "+++ b/new.txt", "\n+a\n+b"} {
		if !strings.Contains(d.Patch, s) {
			t.Errorf("Patch missing %q:\n%s", s, d.Patch)
		}
	}
}

func TestDiffCurrentBranchTicketShowsUncommittedWork(t *testing.T) {
	s := diffScript("", "", "")
	m := newTestManager(s, t.TempDir())

	if _, err := m.Diff(context.Background(), store.Ticket{ID: "AGT-01", BranchMode: store.BranchModeCurrent}); err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if _, ok := s.findCall("merge-base"); ok {
		t.Error("current-branch ticket should not look for a fork point")
	}
	d, ok := s.findCall("diff", "--no-color")
	if !ok || d.dir != "/repo" || d.args[len(d.args)-1] != "HEAD" {
		t.Errorf("diff call = %+v, want diff against HEAD in /repo", d)
	}
}

func TestDiffWorktreeNotYetCreatedIsEmpty(t *testing.T) {
	for name, path := range map[string]string{"never started": "", "removed": "/nonexistent/AGT-01"} {
		t.Run(name, func(t *testing.T) {
			s := diffScript("1\t0\tx", "PATCH", "")
			m := newTestManager(s, t.TempDir())

			d, err := m.Diff(context.Background(), store.Ticket{ID: "AGT-01", WorktreePath: path})
			if err != nil {
				t.Fatalf("Diff: %v", err)
			}
			if len(d.Files) != 0 || d.Patch != "" {
				t.Errorf("Diff = %+v, want empty", d)
			}
			if len(s.calls) != 0 {
				t.Errorf("git called for a missing worktree: %v", s.calls)
			}
		})
	}
}

func TestDiffUntrackedBinaryIsNotInlined(t *testing.T) {
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, "logo.png"), []byte{0x89, 'P', 'N', 'G', 0, 1, 2}, 0644); err != nil {
		t.Fatal(err)
	}
	m := newTestManager(diffScript("", "", "logo.png"), t.TempDir())

	d, err := m.Diff(context.Background(), store.Ticket{ID: "AGT-01", WorktreePath: wt})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(d.Files) != 1 || !d.Files[0].Binary || !d.Files[0].Untracked {
		t.Errorf("Files = %+v, want one untracked binary", d.Files)
	}
	if !strings.Contains(d.Patch, "Binary file logo.png") || strings.Contains(d.Patch, "PNG") {
		t.Errorf("Patch should name the binary without its bytes:\n%q", d.Patch)
	}
}
