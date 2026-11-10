package store

import (
	"context"
	"fmt"
	"time"
)

// LinkedRepo is another repository whose board this one shows: a workspace
// board's tickets for that repository live in the repository's own board.
type LinkedRepo struct {
	Path string
	Key  string
}

func (s *Store) LinkRepo(ctx context.Context, r LinkedRepo) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO linked_repos (path, project_key, linked_at) VALUES (?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET project_key = excluded.project_key`,
		r.Path, r.Key, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("store.linkRepo: %w", err)
	}
	return nil
}

func (s *Store) UnlinkRepo(ctx context.Context, path string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM linked_repos WHERE path = ?", path); err != nil {
		return fmt.Errorf("store.unlinkRepo: %w", err)
	}
	return nil
}

func (s *Store) ListLinkedRepos(ctx context.Context) ([]LinkedRepo, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT path, project_key FROM linked_repos ORDER BY rowid")
	if err != nil {
		return nil, fmt.Errorf("store.listLinkedRepos: %w", err)
	}
	defer rows.Close()
	var out []LinkedRepo
	for rows.Next() {
		var r LinkedRepo
		if err := rows.Scan(&r.Path, &r.Key); err != nil {
			return nil, fmt.Errorf("store.listLinkedRepos: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
