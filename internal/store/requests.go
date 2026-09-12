package store

import (
	"context"
	"fmt"
	"time"
)

// PendingRequest is one permission request the receiver held open while a
// human decided. It outlives the hook so the board can show what an agent was
// waiting on, and for how long, after the connection that carried it is gone.
type PendingRequest struct {
	ID         string
	SessionID  string
	TicketID   string
	Kind       string
	ToolName   string
	ToolInput  string
	CreatedAt  time.Time
	ResolvedAt *time.Time
	Decision   string
}

// DecisionAbandoned marks a request no hook is holding any more: the board
// restarted while it was open, so the agent fell back to its own pane picker.
const DecisionAbandoned = "abandoned"

const requestPrefix = "REQ-"

func (s *Store) nextRequestID(ctx context.Context) (string, error) {
	s.db.ExecContext(ctx, "INSERT OR IGNORE INTO id_counters (prefix, next_id) VALUES (?, 1)", requestPrefix)
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT next_id FROM id_counters WHERE prefix = ?", requestPrefix).Scan(&n); err != nil {
		return "", fmt.Errorf("store.nextRequestID: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE id_counters SET next_id = next_id + 1 WHERE prefix = ?", requestPrefix); err != nil {
		return "", fmt.Errorf("store.nextRequestID: %w", err)
	}
	return fmt.Sprintf("%s%02d", requestPrefix, n), nil
}

// CreatePendingRequest records an open request. An empty TicketID is filled
// from the session, since the receiver that raises requests knows only the
// session id.
func (s *Store) CreatePendingRequest(ctx context.Context, r PendingRequest) (PendingRequest, error) {
	id, err := s.nextRequestID(ctx)
	if err != nil {
		return PendingRequest{}, err
	}
	r.ID = id
	r.CreatedAt = time.Now()
	r.ResolvedAt = nil
	r.Decision = ""
	if r.TicketID == "" {
		_ = s.db.QueryRowContext(ctx, "SELECT ticket_id FROM sessions WHERE id = ?", r.SessionID).Scan(&r.TicketID)
	}
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO pending_requests (id, session_id, ticket_id, kind, tool_name, tool_input, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		r.ID, r.SessionID, r.TicketID, r.Kind, r.ToolName, r.ToolInput, r.CreatedAt,
	)
	if err != nil {
		return PendingRequest{}, fmt.Errorf("store.createPendingRequest: %w", err)
	}
	return r, nil
}

// ResolvePendingRequest closes an open request with the decision it got. A
// request that is already closed, or unknown, is ErrNotFound.
func (s *Store) ResolvePendingRequest(ctx context.Context, id, decision string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE pending_requests SET resolved_at = ?, decision = ? WHERE id = ? AND resolved_at IS NULL",
		time.Now(), decision, id,
	)
	if err != nil {
		return fmt.Errorf("store.resolvePendingRequest: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store.resolvePendingRequest %s: %w", id, ErrNotFound)
	}
	return nil
}

// AbandonPendingRequests closes every open request as abandoned and reports
// how many there were. The receiver calls it at startup: a request left open
// by a previous process has no hook waiting on it any more.
func (s *Store) AbandonPendingRequests(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx,
		"UPDATE pending_requests SET resolved_at = ?, decision = ? WHERE resolved_at IS NULL",
		time.Now(), DecisionAbandoned,
	)
	if err != nil {
		return 0, fmt.Errorf("store.abandonPendingRequests: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ListPendingRequests returns the open requests, oldest first.
func (s *Store) ListPendingRequests(ctx context.Context) ([]PendingRequest, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, session_id, ticket_id, kind, tool_name, tool_input, created_at FROM pending_requests WHERE resolved_at IS NULL ORDER BY created_at ASC, id ASC",
	)
	if err != nil {
		return nil, fmt.Errorf("store.listPendingRequests: %w", err)
	}
	defer rows.Close()

	var out []PendingRequest
	for rows.Next() {
		var r PendingRequest
		if err := rows.Scan(&r.ID, &r.SessionID, &r.TicketID, &r.Kind, &r.ToolName, &r.ToolInput, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("store.listPendingRequests: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
