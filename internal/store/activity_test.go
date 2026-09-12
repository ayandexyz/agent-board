package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSessionActivityRoundTrip(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()
	ctx := context.Background()
	sess, err := s.CreateSession(ctx, Session{TicketID: "AGT-01", Agent: "claude-code", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLaunchID(ctx, sess.ID, "L1"); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	if err := s.SetActivity(ctx, sess.ID, "blocked", at); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListActivity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("ListActivity = %+v, want one row", list)
	}
	got := list[0]
	if got.SessionID != sess.ID || got.State != "blocked" || got.LaunchID != "L1" || !got.ActivityAt.Equal(at) || got.TicketID != "AGT-01" || got.Agent != "claude-code" {
		t.Fatalf("ListActivity[0] = %+v", got)
	}
	if err := s.EndSession(ctx, sess.ID, "completed"); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListActivity(ctx)
	if len(list) != 0 {
		t.Fatalf("ended session still listed: %+v", list)
	}
}

func TestSessionActivityNeverReported(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()
	ctx := context.Background()
	if _, err := s.CreateSession(ctx, Session{TicketID: "AGT-01", Agent: "opencode", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListActivity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].State != "" || !list[0].ActivityAt.IsZero() || list[0].LaunchID != "" {
		t.Fatalf("a session that never reported should list with empty activity, got %+v", list)
	}
}

func TestSetActivityUnknownSession(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()
	ctx := context.Background()
	if err := s.SetActivity(ctx, "SES-99", "active", time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetActivity unknown = %v, want ErrNotFound", err)
	}
	if err := s.SetLaunchID(ctx, "SES-99", "L1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetLaunchID unknown = %v, want ErrNotFound", err)
	}
}

func TestPendingRequestLifecycle(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()
	ctx := context.Background()
	r, err := s.CreatePendingRequest(ctx, PendingRequest{SessionID: "SES-01", TicketID: "AGT-01", Kind: "permission", ToolName: "Bash", ToolInput: `{"command":"rm -rf build"}`})
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "REQ-01" {
		t.Fatalf("ID = %q", r.ID)
	}
	if r.CreatedAt.IsZero() {
		t.Fatal("CreatedAt not set")
	}
	open, _ := s.ListPendingRequests(ctx)
	if len(open) != 1 || open[0].ToolName != "Bash" || open[0].ToolInput != `{"command":"rm -rf build"}` || open[0].TicketID != "AGT-01" {
		t.Fatalf("open = %+v", open)
	}
	if err := s.ResolvePendingRequest(ctx, r.ID, "allow"); err != nil {
		t.Fatal(err)
	}
	open, _ = s.ListPendingRequests(ctx)
	if len(open) != 0 {
		t.Fatalf("resolved request still open: %+v", open)
	}
	if err := s.ResolvePendingRequest(ctx, r.ID, "deny"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("resolving twice = %v, want ErrNotFound", err)
	}
	if err := s.ResolvePendingRequest(ctx, "REQ-99", "allow"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("resolving unknown = %v, want ErrNotFound", err)
	}
	r2, _ := s.CreatePendingRequest(ctx, PendingRequest{SessionID: "SES-01", Kind: "permission"})
	if r2.ID != "REQ-02" {
		t.Fatalf("second ID = %q, want REQ-02", r2.ID)
	}
}

func TestPendingRequestTicketFromSession(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()
	ctx := context.Background()
	sess, err := s.CreateSession(ctx, Session{TicketID: "AGT-07", Agent: "claude-code", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CreatePendingRequest(ctx, PendingRequest{SessionID: sess.ID, Kind: "permission", ToolName: "Edit"})
	if err != nil {
		t.Fatal(err)
	}
	if r.TicketID != "AGT-07" {
		t.Fatalf("TicketID = %q, want the session's ticket", r.TicketID)
	}
}

func TestAbandonPendingRequests(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()
	ctx := context.Background()
	if _, err := s.CreatePendingRequest(ctx, PendingRequest{SessionID: "SES-01", Kind: "permission", ToolName: "Bash"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePendingRequest(ctx, PendingRequest{SessionID: "SES-02", Kind: "permission", ToolName: "Edit"}); err != nil {
		t.Fatal(err)
	}
	n, err := s.AbandonPendingRequests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("abandoned %d, want 2", n)
	}
	open, _ := s.ListPendingRequests(ctx)
	if len(open) != 0 {
		t.Fatalf("still open after abandon: %+v", open)
	}
	n, _ = s.AbandonPendingRequests(ctx)
	if n != 0 {
		t.Fatalf("second abandon touched %d rows", n)
	}
}

func TestActivityMigrationIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/board.db"
	statuses := []string{"backlog", "in_progress", "review", "done"}
	s, err := Open(path, statuses, "AGT-")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, statuses, "AGT-")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	if _, err := s.ListActivity(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListPendingRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
}
