package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestOpenAddsWorktreeColumns(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	statuses := []string{"backlog", "in_progress", "review", "done"}

	s, err := Open(dbPath, statuses, "AGT-")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.Close()

	s, err = Open(dbPath, statuses, "AGT-")
	if err != nil {
		t.Fatalf("Open second time: %v", err)
	}
	defer s.Close()

	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer raw.Close()

	for _, col := range []string{"worktree_path", "pr_url"} {
		var count int
		err := raw.QueryRow("SELECT COUNT(*) FROM pragma_table_info('tickets') WHERE name=?", col).Scan(&count)
		if err != nil {
			t.Fatalf("pragma query: %v", err)
		}
		if count != 1 {
			t.Errorf("column %s missing from tickets table", col)
		}
	}
}

func TestSetWorktreePath(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	ticket, err := s.CreateTicket(ctx, Ticket{Title: "Test", Status: "backlog"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	if err := s.SetWorktreePath(ctx, ticket.ID, "/wt/AGT-01"); err != nil {
		t.Fatalf("SetWorktreePath: %v", err)
	}

	got, err := s.GetTicket(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.WorktreePath != "/wt/AGT-01" {
		t.Errorf("WorktreePath = %q, want %q", got.WorktreePath, "/wt/AGT-01")
	}

	if err := s.SetWorktreePath(ctx, ticket.ID, ""); err != nil {
		t.Fatalf("SetWorktreePath clear: %v", err)
	}
	got, _ = s.GetTicket(ctx, ticket.ID)
	if got.WorktreePath != "" {
		t.Errorf("WorktreePath after clear = %q, want empty", got.WorktreePath)
	}
}

func TestSetWorktreePathNotFound(t *testing.T) {
	s := openTestDB(t)

	err := s.SetWorktreePath(context.Background(), "AGT-99", "/wt")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("SetWorktreePath missing ticket err = %v, want ErrNotFound", err)
	}
}

func TestSetPRURL(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	ticket, err := s.CreateTicket(ctx, Ticket{Title: "Test", Status: "backlog"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	if err := s.SetPRURL(ctx, ticket.ID, "https://github.com/u/r/pull/7"); err != nil {
		t.Fatalf("SetPRURL: %v", err)
	}

	got, err := s.GetTicket(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.PRURL != "https://github.com/u/r/pull/7" {
		t.Errorf("PRURL = %q, unexpected", got.PRURL)
	}
}

func TestSetPRURLNotFound(t *testing.T) {
	s := openTestDB(t)

	err := s.SetPRURL(context.Background(), "AGT-99", "https://example.com")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("SetPRURL missing ticket err = %v, want ErrNotFound", err)
	}
}

func TestSetBranch(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	ticket, err := s.CreateTicket(ctx, Ticket{Title: "Test", Status: "backlog"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	if err := s.SetBranch(ctx, ticket.ID, "agent/agt-01"); err != nil {
		t.Fatalf("SetBranch: %v", err)
	}

	got, err := s.GetTicket(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.Branch != "agent/agt-01" {
		t.Errorf("Branch = %q, want agent/agt-01", got.Branch)
	}
}

func TestSetBranchNotFound(t *testing.T) {
	s := openTestDB(t)

	err := s.SetBranch(context.Background(), "AGT-99", "b")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("SetBranch missing ticket err = %v, want ErrNotFound", err)
	}
}

func TestWorktreeFieldsRoundTripThroughUpdateAndList(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	ticket, err := s.CreateTicket(ctx, Ticket{Title: "Test", Status: "backlog", WorktreePath: "/wt/x", PRURL: "https://x"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	list, err := s.ListTickets(ctx, TicketFilters{})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(list) != 1 || list[0].WorktreePath != "/wt/x" || list[0].PRURL != "https://x" {
		t.Errorf("ListTickets round trip = %+v, want worktree/pr fields preserved", list)
	}

	ticket.WorktreePath = "/wt/y"
	ticket.PRURL = ""
	updated, err := s.UpdateTicket(ctx, ticket)
	if err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	got, _ := s.GetTicket(ctx, updated.ID)
	if got.WorktreePath != "/wt/y" || got.PRURL != "" {
		t.Errorf("after update: WorktreePath=%q PRURL=%q, want /wt/y and empty", got.WorktreePath, got.PRURL)
	}
}
