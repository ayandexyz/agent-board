package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenSetsPragmas(t *testing.T) {
	s := openTestDB(t)
	defer s.Close()

	var journal string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatal(err)
	}
	if strings.ToLower(journal) != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journal)
	}
	var timeout int
	if err := s.db.QueryRow("PRAGMA busy_timeout").Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if timeout < 1000 {
		t.Fatalf("busy_timeout = %d ms, want at least a second", timeout)
	}
	var fk int
	if err := s.db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys = %d, want 1", fk)
	}
}

func TestSecondProcessWaitsForTheWriteLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	statuses := []string{"backlog", "in_progress", "review", "done"}
	first, err := Open(path, statuses, "AGT-")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(path, statuses, "AGT-")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	ctx := context.Background()
	tx, err := first.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO meta (key, value) VALUES ('held', '1')"); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := second.CreateTicket(ctx, Ticket{Title: "from the other process", Status: "backlog"})
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("second writer returned immediately with %v; it should have waited for the lock", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second writer failed after the lock was released: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second writer never completed")
	}
}
