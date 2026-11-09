package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ayandexyz/agent-board/internal/git"
	"github.com/ayandexyz/agent-board/internal/orchestrator"
	"github.com/ayandexyz/agent-board/internal/store"
)

type gitCall struct {
	dir  string
	name string
	args []string
}

type gitScript struct {
	calls   []gitCall
	respond func(c gitCall) (string, error)
}

func (s *gitScript) runner() git.Runner {
	return func(ctx context.Context, dir, name string, args ...string) (string, error) {
		c := gitCall{dir: dir, name: name, args: args}
		s.calls = append(s.calls, c)
		if s.respond == nil {
			return "", nil
		}
		return s.respond(c)
	}
}

func (s *gitScript) findCall(prefix ...string) (gitCall, bool) {
	for _, c := range s.calls {
		if len(c.args) < len(prefix) {
			continue
		}
		match := true
		for i, p := range prefix {
			if c.args[i] != p {
				match = false
				break
			}
		}
		if match {
			return c, true
		}
	}
	return gitCall{}, false
}

func newTestManager(s *gitScript, worktreeRoot string) *orchestrator.WorktreeManager {
	g := git.New(s.runner())
	gh := git.NewGH(s.runner())
	return orchestrator.NewWorktreeManager(g, gh, "/repo", worktreeRoot, "agent/", "main")
}

func TestEnsureCreatesWorktreeWithGeneratedBranch(t *testing.T) {
	s := &gitScript{respond: func(c gitCall) (string, error) {
		switch {
		case c.args[0] == "worktree" && c.args[1] == "list":
			return "worktree /repo", nil
		case c.args[0] == "rev-parse" && c.args[1] == "--verify":
			return "", errors.New("exit status 1")
		}
		return "", nil
	}}
	m := newTestManager(s, t.TempDir())

	info, err := m.Ensure(context.Background(), store.Ticket{ID: "AGT-01"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if info.Branch != "agent/agt-01" {
		t.Errorf("Branch = %q, want agent/agt-01", info.Branch)
	}
	if !info.Created {
		t.Error("Created should be true")
	}
	if !strings.HasSuffix(info.Path, "/AGT-01") {
		t.Errorf("Path = %q, want suffix /AGT-01", info.Path)
	}

	add, ok := s.findCall("worktree", "add")
	if !ok {
		t.Fatalf("no worktree add call in %v", s.calls)
	}
	if add.dir != "/repo" {
		t.Errorf("worktree add dir = %q, want /repo", add.dir)
	}
	wantTail := []string{"-b", "agent/agt-01", "main"}
	gotTail := add.args[len(add.args)-3:]
	for i := range wantTail {
		if gotTail[i] != wantTail[i] {
			t.Errorf("worktree add args tail = %v, want %v", gotTail, wantTail)
			break
		}
	}
}

func TestEnsureRespectsTicketBranch(t *testing.T) {
	s := &gitScript{respond: func(c gitCall) (string, error) {
		switch {
		case c.args[0] == "worktree" && c.args[1] == "list":
			return "worktree /repo", nil
		case c.args[0] == "rev-parse" && c.args[1] == "--verify":
			return "", errors.New("exit status 1")
		}
		return "", nil
	}}
	m := newTestManager(s, t.TempDir())

	info, err := m.Ensure(context.Background(), store.Ticket{ID: "AGT-02", Branch: "feature/custom"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if info.Branch != "feature/custom" {
		t.Errorf("Branch = %q, want feature/custom", info.Branch)
	}
}

func TestEnsureExistingBranchNoCreateFlag(t *testing.T) {
	s := &gitScript{respond: func(c gitCall) (string, error) {
		switch {
		case c.args[0] == "worktree" && c.args[1] == "list":
			return "worktree /repo", nil
		case c.args[0] == "rev-parse" && c.args[1] == "--verify":
			return "abc123", nil
		}
		return "", nil
	}}
	m := newTestManager(s, t.TempDir())

	if _, err := m.Ensure(context.Background(), store.Ticket{ID: "AGT-03"}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	add, ok := s.findCall("worktree", "add")
	if !ok {
		t.Fatalf("no worktree add call in %v", s.calls)
	}
	for _, a := range add.args {
		if a == "-b" {
			t.Errorf("worktree add should not create branch, args = %v", add.args)
		}
	}
}

func TestEnsureReusesRegisteredWorktree(t *testing.T) {
	root := t.TempDir()
	s := &gitScript{}
	s.respond = func(c gitCall) (string, error) {
		if c.args[0] == "worktree" && c.args[1] == "list" {
			return "worktree /repo\n\nworktree " + root + "/AGT-04", nil
		}
		return "", nil
	}
	m := newTestManager(s, root)

	info, err := m.Ensure(context.Background(), store.Ticket{ID: "AGT-04"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if info.Created {
		t.Error("Created should be false for registered worktree")
	}
	if _, ok := s.findCall("worktree", "add"); ok {
		t.Error("worktree add should not be called for registered worktree")
	}
}

func TestRemoveRefusesDirtyWorktree(t *testing.T) {
	s := &gitScript{respond: func(c gitCall) (string, error) {
		if c.args[0] == "status" {
			return " M foo.go", nil
		}
		return "", nil
	}}
	m := newTestManager(s, t.TempDir())

	err := m.Remove(context.Background(), "/wt/AGT-01", false)
	if !errors.Is(err, git.ErrDirty) {
		t.Fatalf("Remove dirty err = %v, want ErrDirty", err)
	}
	if _, ok := s.findCall("worktree", "remove"); ok {
		t.Error("worktree remove should not run on dirty worktree")
	}
}

func TestRemoveForcedSkipsDirtyCheck(t *testing.T) {
	s := &gitScript{}
	m := newTestManager(s, t.TempDir())

	if err := m.Remove(context.Background(), "/wt/AGT-01", true); err != nil {
		t.Fatalf("Remove forced: %v", err)
	}
	if _, ok := s.findCall("worktree", "remove"); !ok {
		t.Errorf("worktree remove should run, calls = %v", s.calls)
	}
}
