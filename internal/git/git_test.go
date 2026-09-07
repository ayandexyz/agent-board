package git_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ayan-de/agent-board/internal/git"
)

type call struct {
	dir  string
	name string
	args []string
}

type script struct {
	calls   []call
	respond func(c call) (string, error)
}

func (s *script) runner() git.Runner {
	return func(ctx context.Context, dir, name string, args ...string) (string, error) {
		c := call{dir: dir, name: name, args: args}
		s.calls = append(s.calls, c)
		if s.respond == nil {
			return "", nil
		}
		return s.respond(c)
	}
}

func (s *script) assertCall(t *testing.T, i int, dir string, args ...string) {
	t.Helper()
	if i >= len(s.calls) {
		t.Fatalf("expected call %d, only %d calls made: %v", i, len(s.calls), s.calls)
	}
	c := s.calls[i]
	if c.name != "git" {
		t.Errorf("call %d name = %q, want %q", i, c.name, "git")
	}
	if c.dir != dir {
		t.Errorf("call %d dir = %q, want %q", i, c.dir, dir)
	}
	if !reflect.DeepEqual(c.args, args) {
		t.Errorf("call %d args = %v, want %v", i, c.args, args)
	}
}

func TestRepoRoot(t *testing.T) {
	s := &script{respond: func(c call) (string, error) { return "/home/u/repo", nil }}
	g := git.New(s.runner())

	root, err := g.RepoRoot(context.Background(), "/home/u/repo/sub")
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	if root != "/home/u/repo" {
		t.Errorf("root = %q, want %q", root, "/home/u/repo")
	}
	s.assertCall(t, 0, "/home/u/repo/sub", "rev-parse", "--show-toplevel")
}

func TestRepoRootError(t *testing.T) {
	s := &script{respond: func(c call) (string, error) { return "", errors.New("not a git repository") }}
	g := git.New(s.runner())

	_, err := g.RepoRoot(context.Background(), "/tmp")
	if err == nil {
		t.Fatal("RepoRoot should propagate error")
	}
	if !strings.Contains(err.Error(), "git.repoRoot") {
		t.Errorf("error %q should be wrapped with git.repoRoot", err)
	}
}

func TestCurrentBranch(t *testing.T) {
	s := &script{respond: func(c call) (string, error) { return "main", nil }}
	g := git.New(s.runner())

	branch, err := g.CurrentBranch(context.Background(), "/repo")
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want main", branch)
	}
	s.assertCall(t, 0, "/repo", "rev-parse", "--abbrev-ref", "HEAD")
}

func TestIsDirty(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{"clean tree", "", false},
		{"dirty tree", " M internal/foo.go\n?? bar.go", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &script{respond: func(c call) (string, error) { return tt.output, nil }}
			g := git.New(s.runner())

			dirty, err := g.IsDirty(context.Background(), "/wt")
			if err != nil {
				t.Fatalf("IsDirty: %v", err)
			}
			if dirty != tt.want {
				t.Errorf("dirty = %v, want %v", dirty, tt.want)
			}
			s.assertCall(t, 0, "/wt", "status", "--porcelain")
		})
	}
}

func TestBranchExists(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"exists", nil, true},
		{"missing", errors.New("exit status 1"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &script{respond: func(c call) (string, error) { return "", tt.err }}
			g := git.New(s.runner())

			got, err := g.BranchExists(context.Background(), "/repo", "agent/agt-01")
			if err != nil {
				t.Fatalf("BranchExists: %v", err)
			}
			if got != tt.want {
				t.Errorf("exists = %v, want %v", got, tt.want)
			}
			s.assertCall(t, 0, "/repo", "rev-parse", "--verify", "--quiet", "refs/heads/agent/agt-01")
		})
	}
}

func TestDetectBaseBranch(t *testing.T) {
	tests := []struct {
		name    string
		mainErr error
		want    string
		wantErr bool
	}{
		{"main exists", nil, "main", false},
		{"falls back to master", errors.New("exit status 1"), "master", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &script{respond: func(c call) (string, error) {
				if c.args[len(c.args)-1] == "refs/heads/main" {
					return "", tt.mainErr
				}
				return "", nil
			}}
			g := git.New(s.runner())

			got, err := g.DetectBaseBranch(context.Background(), "/repo")
			if (err != nil) != tt.wantErr {
				t.Fatalf("DetectBaseBranch err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("base = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectBaseBranchNeither(t *testing.T) {
	s := &script{respond: func(c call) (string, error) { return "", errors.New("exit status 1") }}
	g := git.New(s.runner())

	_, err := g.DetectBaseBranch(context.Background(), "/repo")
	if err == nil {
		t.Fatal("DetectBaseBranch should error when neither main nor master exists")
	}
}

func TestWorktreeAdd(t *testing.T) {
	tests := []struct {
		name         string
		createBranch bool
		wantArgs     []string
	}{
		{
			name:         "new branch",
			createBranch: true,
			wantArgs:     []string{"worktree", "add", "/wt/AGT-01", "-b", "agent/agt-01", "main"},
		},
		{
			name:         "existing branch",
			createBranch: false,
			wantArgs:     []string{"worktree", "add", "/wt/AGT-01", "agent/agt-01"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &script{}
			g := git.New(s.runner())

			err := g.WorktreeAdd(context.Background(), "/repo", "/wt/AGT-01", "agent/agt-01", "main", tt.createBranch)
			if err != nil {
				t.Fatalf("WorktreeAdd: %v", err)
			}
			s.assertCall(t, 0, "/repo", tt.wantArgs...)
		})
	}
}

func TestWorktreeRemove(t *testing.T) {
	tests := []struct {
		name     string
		force    bool
		wantArgs []string
	}{
		{"normal", false, []string{"worktree", "remove", "/wt/AGT-01"}},
		{"forced", true, []string{"worktree", "remove", "--force", "/wt/AGT-01"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &script{}
			g := git.New(s.runner())

			if err := g.WorktreeRemove(context.Background(), "/repo", "/wt/AGT-01", tt.force); err != nil {
				t.Fatalf("WorktreeRemove: %v", err)
			}
			s.assertCall(t, 0, "/repo", tt.wantArgs...)
		})
	}
}

func TestWorktreeList(t *testing.T) {
	out := "worktree /repo\nHEAD abc\nbranch refs/heads/main\n\nworktree /wt/AGT-01\nHEAD def\nbranch refs/heads/agent/agt-01\n"
	s := &script{respond: func(c call) (string, error) { return out, nil }}
	g := git.New(s.runner())

	paths, err := g.WorktreeList(context.Background(), "/repo")
	if err != nil {
		t.Fatalf("WorktreeList: %v", err)
	}
	want := []string{"/repo", "/wt/AGT-01"}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
	s.assertCall(t, 0, "/repo", "worktree", "list", "--porcelain")
}

func TestRebaseSuccess(t *testing.T) {
	s := &script{}
	g := git.New(s.runner())

	if err := g.Rebase(context.Background(), "/wt/AGT-01", "main"); err != nil {
		t.Fatalf("Rebase: %v", err)
	}
	s.assertCall(t, 0, "/wt/AGT-01", "rebase", "main")
}

func TestRebaseConflictAbortsAndClassifies(t *testing.T) {
	s := &script{respond: func(c call) (string, error) {
		if len(c.args) > 0 && c.args[0] == "rebase" && len(c.args) == 2 && c.args[1] == "main" {
			return "CONFLICT (content): Merge conflict in foo.go", errors.New("exit status 1")
		}
		return "", nil
	}}
	g := git.New(s.runner())

	err := g.Rebase(context.Background(), "/wt/AGT-01", "main")
	if !errors.Is(err, git.ErrConflict) {
		t.Fatalf("Rebase error = %v, want ErrConflict", err)
	}
	if !strings.Contains(err.Error(), "foo.go") {
		t.Errorf("error should carry git output, got %q", err)
	}
	s.assertCall(t, 1, "/wt/AGT-01", "rebase", "--abort")
}

func TestMergeFFOnly(t *testing.T) {
	s := &script{}
	g := git.New(s.runner())

	if err := g.MergeFFOnly(context.Background(), "/repo", "agent/agt-01"); err != nil {
		t.Fatalf("MergeFFOnly: %v", err)
	}
	s.assertCall(t, 0, "/repo", "merge", "--ff-only", "agent/agt-01")
}

func TestMergeFFOnlyNotFastForward(t *testing.T) {
	s := &script{respond: func(c call) (string, error) {
		return "fatal: Not possible to fast-forward, aborting.", errors.New("exit status 128")
	}}
	g := git.New(s.runner())

	err := g.MergeFFOnly(context.Background(), "/repo", "agent/agt-01")
	if !errors.Is(err, git.ErrNotFastForward) {
		t.Fatalf("MergeFFOnly error = %v, want ErrNotFastForward", err)
	}
}

func TestPush(t *testing.T) {
	s := &script{}
	g := git.New(s.runner())

	if err := g.Push(context.Background(), "/wt/AGT-01", "origin", "agent/agt-01"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	s.assertCall(t, 0, "/wt/AGT-01", "push", "-u", "origin", "agent/agt-01")
}

func TestDeleteBranch(t *testing.T) {
	tests := []struct {
		name     string
		force    bool
		wantArgs []string
	}{
		{"merged delete", false, []string{"branch", "-d", "agent/agt-01"}},
		{"force delete", true, []string{"branch", "-D", "agent/agt-01"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &script{}
			g := git.New(s.runner())

			if err := g.DeleteBranch(context.Background(), "/repo", "agent/agt-01", tt.force); err != nil {
				t.Fatalf("DeleteBranch: %v", err)
			}
			s.assertCall(t, 0, "/repo", tt.wantArgs...)
		})
	}
}

func TestIsAncestor(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"is ancestor", nil, true},
		{"not ancestor", errors.New("exit status 1"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &script{respond: func(c call) (string, error) { return "", tt.err }}
			g := git.New(s.runner())

			got, err := g.IsAncestor(context.Background(), "/repo", "agent/agt-01", "main")
			if err != nil {
				t.Fatalf("IsAncestor: %v", err)
			}
			if got != tt.want {
				t.Errorf("IsAncestor = %v, want %v", got, tt.want)
			}
			s.assertCall(t, 0, "/repo", "merge-base", "--is-ancestor", "agent/agt-01", "main")
		})
	}
}
