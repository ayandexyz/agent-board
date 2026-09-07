package git_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ayan-de/agent-board/internal/git"
)

func TestPRCreate(t *testing.T) {
	s := &script{respond: func(c call) (string, error) {
		return "Creating pull request for agent/agt-01 into main\nhttps://github.com/u/repo/pull/7", nil
	}}
	gh := git.NewGH(s.runner())

	url, err := gh.PRCreate(context.Background(), "/wt/AGT-01", "main", "agent/agt-01", "AGT-01: Add feature", "body text")
	if err != nil {
		t.Fatalf("PRCreate: %v", err)
	}
	if url != "https://github.com/u/repo/pull/7" {
		t.Errorf("url = %q, want the PR url", url)
	}

	if len(s.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(s.calls))
	}
	c := s.calls[0]
	if c.name != "gh" {
		t.Errorf("name = %q, want gh", c.name)
	}
	if c.dir != "/wt/AGT-01" {
		t.Errorf("dir = %q, want /wt/AGT-01", c.dir)
	}
	want := []string{"pr", "create", "--base", "main", "--head", "agent/agt-01", "--title", "AGT-01: Add feature", "--body", "body text"}
	if !reflect.DeepEqual(c.args, want) {
		t.Errorf("args = %v, want %v", c.args, want)
	}
}

func TestPRCreateError(t *testing.T) {
	s := &script{respond: func(c call) (string, error) {
		return "a pull request already exists", errors.New("exit status 1")
	}}
	gh := git.NewGH(s.runner())

	_, err := gh.PRCreate(context.Background(), "/wt", "main", "b", "t", "b")
	if err == nil {
		t.Fatal("PRCreate should propagate error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should carry gh output, got %q", err)
	}
}

func TestPRView(t *testing.T) {
	s := &script{respond: func(c call) (string, error) {
		return `{"state":"MERGED","url":"https://github.com/u/repo/pull/7"}`, nil
	}}
	gh := git.NewGH(s.runner())

	info, err := gh.PRView(context.Background(), "/repo", "agent/agt-01")
	if err != nil {
		t.Fatalf("PRView: %v", err)
	}
	if info.State != "MERGED" {
		t.Errorf("State = %q, want MERGED", info.State)
	}
	if info.URL != "https://github.com/u/repo/pull/7" {
		t.Errorf("URL = %q, unexpected", info.URL)
	}

	c := s.calls[0]
	want := []string{"pr", "view", "agent/agt-01", "--json", "url,state"}
	if c.name != "gh" || !reflect.DeepEqual(c.args, want) {
		t.Errorf("call = %s %v, want gh %v", c.name, c.args, want)
	}
}

func TestPRViewNoPR(t *testing.T) {
	s := &script{respond: func(c call) (string, error) {
		return "no pull requests found for branch", errors.New("exit status 1")
	}}
	gh := git.NewGH(s.runner())

	_, err := gh.PRView(context.Background(), "/repo", "agent/agt-01")
	if err == nil {
		t.Fatal("PRView should error when no PR exists")
	}
}
