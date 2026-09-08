package store

import (
	"context"
	"path/filepath"
	"testing"
)

func openBranchModeStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"),
		[]string{"backlog", "in_progress", "review", "done"}, "AGT-")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateTicketDefaultsToWorktreeBranchMode(t *testing.T) {
	s := openBranchModeStore(t)

	created, err := s.CreateTicket(context.Background(), Ticket{Title: "T", Status: "backlog"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if created.BranchMode != BranchModeWorktree {
		t.Errorf("created.BranchMode = %q, want %q", created.BranchMode, BranchModeWorktree)
	}

	got, err := s.GetTicket(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.BranchMode != BranchModeWorktree {
		t.Errorf("BranchMode = %q, want %q", got.BranchMode, BranchModeWorktree)
	}
	if !got.UsesWorktree() {
		t.Error("UsesWorktree() = false, want true for default ticket")
	}
}

func TestTicketBranchModeCurrentRoundTrips(t *testing.T) {
	s := openBranchModeStore(t)
	ctx := context.Background()

	created, err := s.CreateTicket(ctx, Ticket{Title: "T", Status: "backlog", BranchMode: BranchModeCurrent})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	got, err := s.GetTicket(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.BranchMode != BranchModeCurrent {
		t.Fatalf("BranchMode = %q, want %q", got.BranchMode, BranchModeCurrent)
	}
	if got.UsesWorktree() {
		t.Error("UsesWorktree() = true, want false for current-branch ticket")
	}

	got.BranchMode = BranchModeWorktree
	if _, err := s.UpdateTicket(ctx, got); err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	back, err := s.GetTicket(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if back.BranchMode != BranchModeWorktree {
		t.Errorf("after update BranchMode = %q, want %q", back.BranchMode, BranchModeWorktree)
	}

	list, err := s.ListTickets(ctx, TicketFilters{})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(list) != 1 || list[0].BranchMode != BranchModeWorktree {
		t.Errorf("ListTickets branch mode = %+v", list)
	}
}
