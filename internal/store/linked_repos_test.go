package store

import (
	"context"
	"testing"
)

func TestLinkRepoRecordsAndListsInOrder(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()
	ctx := context.Background()

	if err := s.LinkRepo(ctx, LinkedRepo{Path: "/src/freecode", Key: "ayandexyz-freecode"}); err != nil {
		t.Fatalf("LinkRepo: %v", err)
	}
	if err := s.LinkRepo(ctx, LinkedRepo{Path: "/src/agent-board", Key: "ayandexyz-agent-board"}); err != nil {
		t.Fatalf("LinkRepo: %v", err)
	}

	got, err := s.ListLinkedRepos(ctx)
	if err != nil {
		t.Fatalf("ListLinkedRepos: %v", err)
	}
	want := []LinkedRepo{
		{Path: "/src/freecode", Key: "ayandexyz-freecode"},
		{Path: "/src/agent-board", Key: "ayandexyz-agent-board"},
	}
	if len(got) != len(want) {
		t.Fatalf("ListLinkedRepos = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ListLinkedRepos[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestLinkRepoTwiceKeepsOneRowAndUpdatesKey(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()
	ctx := context.Background()

	if err := s.LinkRepo(ctx, LinkedRepo{Path: "/src/freecode", Key: "old-key"}); err != nil {
		t.Fatalf("LinkRepo: %v", err)
	}
	if err := s.LinkRepo(ctx, LinkedRepo{Path: "/src/freecode", Key: "new-key"}); err != nil {
		t.Fatalf("LinkRepo again: %v", err)
	}

	got, err := s.ListLinkedRepos(ctx)
	if err != nil {
		t.Fatalf("ListLinkedRepos: %v", err)
	}
	if len(got) != 1 || got[0].Key != "new-key" {
		t.Errorf("ListLinkedRepos = %+v, want one row keyed new-key", got)
	}
}

func TestUnlinkRepoRemovesIt(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()
	ctx := context.Background()

	if err := s.LinkRepo(ctx, LinkedRepo{Path: "/src/freecode", Key: "k"}); err != nil {
		t.Fatalf("LinkRepo: %v", err)
	}
	if err := s.UnlinkRepo(ctx, "/src/freecode"); err != nil {
		t.Fatalf("UnlinkRepo: %v", err)
	}
	got, err := s.ListLinkedRepos(ctx)
	if err != nil {
		t.Fatalf("ListLinkedRepos: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListLinkedRepos = %+v, want none", got)
	}
}

func TestPurgeTicketRecordsKeepsOtherTickets(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()
	ctx := context.Background()

	if _, err := s.CreateSession(ctx, Session{TicketID: "FRE-01", Agent: "claude", Status: "running"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := s.CreateSession(ctx, Session{TicketID: "FRE-02", Agent: "claude", Status: "running"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := s.CreateEvent(ctx, Event{TicketID: "FRE-01", Kind: "worktree.created", Payload: "/w"}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}

	if err := s.PurgeTicketRecords(ctx, "FRE-01"); err != nil {
		t.Fatalf("PurgeTicketRecords: %v", err)
	}

	if s.HasActiveSession(ctx, "FRE-01") {
		t.Error("FRE-01 still has a session after purge")
	}
	if !s.HasActiveSession(ctx, "FRE-02") {
		t.Error("purging FRE-01 removed FRE-02's session")
	}
}
