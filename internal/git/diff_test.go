package git_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/ayandexyz/agent-board/internal/git"
)

func TestMergeBase(t *testing.T) {
	s := &script{respond: func(c call) (string, error) { return "abc123", nil }}
	got, err := git.New(s.runner()).MergeBase(context.Background(), "/wt", "main", "HEAD")
	if err != nil {
		t.Fatalf("MergeBase: %v", err)
	}
	if got != "abc123" {
		t.Errorf("MergeBase = %q, want abc123", got)
	}
	s.assertCall(t, 0, "/wt", "merge-base", "main", "HEAD")
}

func TestDiff(t *testing.T) {
	s := &script{respond: func(c call) (string, error) { return "diff --git a/x b/x", nil }}
	got, err := git.New(s.runner()).Diff(context.Background(), "/wt", "abc123")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if got != "diff --git a/x b/x" {
		t.Errorf("Diff = %q", got)
	}
	s.assertCall(t, 0, "/wt", "diff", "--no-color", "--no-ext-diff", "abc123")
}

func TestDiffNumstat(t *testing.T) {
	s := &script{respond: func(c call) (string, error) {
		return "3\t1\tmain.go\n-\t-\tlogo.png\n0\t12\told/gone.go", nil
	}}
	got, err := git.New(s.runner()).DiffNumstat(context.Background(), "/wt", "abc123")
	if err != nil {
		t.Fatalf("DiffNumstat: %v", err)
	}
	want := []git.FileChange{
		{Path: "main.go", Added: 3, Deleted: 1},
		{Path: "logo.png", Binary: true},
		{Path: "old/gone.go", Deleted: 12},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiffNumstat = %+v, want %+v", got, want)
	}
	s.assertCall(t, 0, "/wt", "diff", "--numstat", "--no-renames", "abc123")
}

func TestDiffNumstatEmpty(t *testing.T) {
	s := &script{}
	got, err := git.New(s.runner()).DiffNumstat(context.Background(), "/wt", "HEAD")
	if err != nil {
		t.Fatalf("DiffNumstat: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("DiffNumstat = %+v, want none", got)
	}
}

func TestUntrackedFiles(t *testing.T) {
	s := &script{respond: func(c call) (string, error) { return "new.go\ndocs/a.md", nil }}
	got, err := git.New(s.runner()).UntrackedFiles(context.Background(), "/wt")
	if err != nil {
		t.Fatalf("UntrackedFiles: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"new.go", "docs/a.md"}) {
		t.Errorf("UntrackedFiles = %v", got)
	}
	s.assertCall(t, 0, "/wt", "ls-files", "--others", "--exclude-standard")
}
