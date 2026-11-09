package orchestrator_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ayandexyz/agent-board/internal/orchestrator"
	"github.com/ayandexyz/agent-board/internal/store"
)

func TestDefaultBranchName(t *testing.T) {
	tests := []struct {
		prefix, id, want string
	}{
		{"agent/", "AGT-07", "agent/agt-07"},
		{"", "AGT-07", "agent/agt-07"},
		{"feature/", "Board-12", "feature/board-12"},
	}
	for _, tc := range tests {
		if got := orchestrator.DefaultBranchName(tc.prefix, tc.id); got != tc.want {
			t.Errorf("DefaultBranchName(%q, %q) = %q, want %q", tc.prefix, tc.id, got, tc.want)
		}
	}
}

func TestCreateTicketLandsInFirstStatus(t *testing.T) {
	fs := &fakeStore{statuses: []string{"backlog", "in_progress", "done"}}
	svc := orchestrator.NewService(fs, nil, nil, nil)

	got, err := svc.CreateTicket(context.Background(), orchestrator.CreateTicketInput{Title: "Add login"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if got.Status != "backlog" {
		t.Errorf("Status = %q, want backlog", got.Status)
	}
	if fs.createdTicket.Title != "Add login" {
		t.Errorf("stored title = %q", fs.createdTicket.Title)
	}
}

func TestCreateTicketRefusesEmptyTitle(t *testing.T) {
	fs := &fakeStore{statuses: []string{"backlog"}}
	svc := orchestrator.NewService(fs, nil, nil, nil)

	if _, err := svc.CreateTicket(context.Background(), orchestrator.CreateTicketInput{}); !errors.Is(err, orchestrator.ErrTitleRequired) {
		t.Fatalf("err = %v, want ErrTitleRequired", err)
	}
}

func TestUpdateTicketKeepsStoredStatus(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Title: "old", Status: "review"}}
	svc := orchestrator.NewService(fs, nil, nil, nil)

	got, err := svc.UpdateTicket(context.Background(), store.Ticket{ID: "AGT-01", Title: "new", Status: "done"})
	if err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	if got.Status != "review" {
		t.Errorf("Status = %q, want review: status only moves through MoveTicketStatus", got.Status)
	}
	if got.Title != "new" {
		t.Errorf("Title = %q, want new", got.Title)
	}
}

func TestSetBranchModeCurrentClearsBranch(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Title: "t", Status: "backlog", Branch: "agent/agt-01", BranchMode: store.BranchModeWorktree}}
	svc := orchestrator.NewService(fs, nil, nil, nil)

	got, err := svc.SetBranchMode(context.Background(), "AGT-01", store.BranchModeCurrent)
	if err != nil {
		t.Fatalf("SetBranchMode: %v", err)
	}
	if got.BranchMode != store.BranchModeCurrent || got.Branch != "" {
		t.Errorf("got mode=%q branch=%q, want current with no branch", got.BranchMode, got.Branch)
	}
}

func TestSetBranchModeWorktreeFillsDefaultBranch(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-07", Title: "t", Status: "backlog", BranchMode: store.BranchModeCurrent}}
	svc := orchestrator.NewService(fs, nil, nil, nil)
	svc.SetBranchPrefix("feature/")

	got, err := svc.SetBranchMode(context.Background(), "AGT-07", store.BranchModeWorktree)
	if err != nil {
		t.Fatalf("SetBranchMode: %v", err)
	}
	if got.BranchMode != store.BranchModeWorktree || got.Branch != "feature/agt-07" {
		t.Errorf("got mode=%q branch=%q, want worktree feature/agt-07", got.BranchMode, got.Branch)
	}
}

func TestSetBranchModeWorktreeKeepsExistingBranch(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-07", Title: "t", Status: "backlog", Branch: "mine", BranchMode: store.BranchModeWorktree}}
	svc := orchestrator.NewService(fs, nil, nil, nil)

	got, err := svc.SetBranchMode(context.Background(), "AGT-07", store.BranchModeWorktree)
	if err != nil {
		t.Fatalf("SetBranchMode: %v", err)
	}
	if got.Branch != "mine" {
		t.Errorf("Branch = %q, want mine", got.Branch)
	}
}

func TestSetBranchModeRejectsUnknownMode(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-07", Status: "backlog"}}
	svc := orchestrator.NewService(fs, nil, nil, nil)

	if _, err := svc.SetBranchMode(context.Background(), "AGT-07", "detached"); !errors.Is(err, orchestrator.ErrInvalidBranchMode) {
		t.Fatalf("err = %v, want ErrInvalidBranchMode", err)
	}
}
