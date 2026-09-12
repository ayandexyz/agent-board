package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound        = errors.New("store: not found")
	ErrInvalidStatus   = errors.New("store: invalid status")
	ErrInvalidPriority = errors.New("store: invalid priority")
)

type Store struct {
	db            *sql.DB
	validStatuses []string
	ticketPrefix  string
}

func Open(dbPath string, validStatuses []string, ticketPrefix string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("store.open: creating dir: %w", err)
	}

	db, err := sql.Open("sqlite", dsn(dbPath))
	if err != nil {
		return nil, fmt.Errorf("store.open: %w", err)
	}

	db.SetMaxOpenConns(1)

	s := &Store{db: db, validStatuses: validStatuses, ticketPrefix: ticketPrefix}

	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Statuses() []string {
	return append([]string(nil), s.validStatuses...)
}

func (s *Store) SetTicketPrefix(prefix string) {
	s.ticketPrefix = prefix
}

// dsn opens the database in WAL mode with a busy timeout, so a second
// agentboard process on the same file — a CLI command while the TUI is up —
// waits for the write lock instead of failing at once with SQLITE_BUSY. The
// pragmas ride on the DSN so every connection the pool opens gets them.
func dsn(path string) string {
	return "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
}
