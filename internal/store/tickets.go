package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

const (
	BranchModeWorktree = "worktree"
	BranchModeCurrent  = "current"
)

type Ticket struct {
	ID            string
	Title         string
	Prompt        string
	Status        string
	Priority      string
	Agent         string
	Branch        string
	BranchMode    string
	WorktreePath  string
	PRURL         string
	Tags          []string
	DependsOn     []string
	AgentActive   bool
	ResumeCommand string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	// Repo is the root of the repository the ticket belongs to when that is
	// not the board's own. It is not a column: a workspace board fills it in
	// from which repository's board the ticket was read.
	Repo string
}

type TicketFilters struct {
	Status   string
	Agent    string
	Priority string
	Tag      string
	From     *time.Time
	To       *time.Time
	Search   string
}

type ticketRow struct {
	ID            string
	Title         string
	Prompt        string
	Status        string
	Priority      string
	Agent         string
	Branch        string
	BranchMode    string
	WorktreePath  string
	PRURL         string
	Tags          string
	DependsOn     string
	AgentActive   bool
	ResumeCommand sql.NullString
	CreatedAt     string
	UpdatedAt     string
}

func (r ticketRow) toTicket() (Ticket, error) {
	var tags []string
	if err := json.Unmarshal([]byte(r.Tags), &tags); err != nil {
		return Ticket{}, err
	}
	var dependsOn []string
	if err := json.Unmarshal([]byte(r.DependsOn), &dependsOn); err != nil {
		return Ticket{}, err
	}

	createdAt, _ := time.Parse(time.RFC3339, r.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339, r.UpdatedAt)

	return Ticket{
		ID:            r.ID,
		Title:         r.Title,
		Prompt:        r.Prompt,
		Status:        r.Status,
		Priority:      r.Priority,
		Agent:         r.Agent,
		Branch:        r.Branch,
		BranchMode:    normalizeBranchMode(r.BranchMode),
		WorktreePath:  r.WorktreePath,
		PRURL:         r.PRURL,
		Tags:          tags,
		DependsOn:     dependsOn,
		AgentActive:   r.AgentActive,
		ResumeCommand: r.ResumeCommand.String,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}, nil
}

func normalizeBranchMode(mode string) string {
	if mode == BranchModeCurrent {
		return BranchModeCurrent
	}
	return BranchModeWorktree
}

func (t Ticket) UsesWorktree() bool {
	return normalizeBranchMode(t.BranchMode) != BranchModeCurrent
}

var validPriorities = map[string]bool{
	"low": true, "medium": true, "high": true, "critical": true,
}

func (s *Store) isValidStatus(status string) bool {
	for _, v := range s.validStatuses {
		if v == status {
			return true
		}
	}
	return false
}

func (s *Store) nextTicketID(ctx context.Context) (string, error) {
	prefix := s.ticketPrefix

	s.db.ExecContext(ctx, "INSERT OR IGNORE INTO id_counters (prefix, next_id) VALUES (?, 1)", prefix)

	var nextID int
	err := s.db.QueryRowContext(ctx, "SELECT next_id FROM id_counters WHERE prefix = ?", prefix).Scan(&nextID)
	if err != nil {
		return "", fmt.Errorf("store.nextTicketID: %w", err)
	}

	_, err = s.db.ExecContext(ctx, "UPDATE id_counters SET next_id = next_id + 1 WHERE prefix = ?", prefix)
	if err != nil {
		return "", fmt.Errorf("store.nextTicketID: %w", err)
	}

	return fmt.Sprintf("%s%02d", prefix, nextID), nil
}

func (s *Store) CreateTicket(ctx context.Context, t Ticket) (Ticket, error) {
	if t.Title == "" {
		return Ticket{}, fmt.Errorf("store.createTicket: title is required")
	}
	if !s.isValidStatus(t.Status) {
		return Ticket{}, fmt.Errorf("store.createTicket: %q: %w", t.Status, ErrInvalidStatus)
	}
	if t.Priority != "" && !validPriorities[t.Priority] {
		return Ticket{}, fmt.Errorf("store.createTicket: %q: %w", t.Priority, ErrInvalidPriority)
	}
	if t.Priority == "" {
		t.Priority = "medium"
	}
	t.BranchMode = normalizeBranchMode(t.BranchMode)

	id, err := s.nextTicketID(ctx)
	if err != nil {
		return Ticket{}, err
	}
	t.ID = id

	tags, err := json.Marshal(t.Tags)
	if err != nil {
		return Ticket{}, fmt.Errorf("store.createTicket: encoding tags: %w", err)
	}
	deps, err := json.Marshal(t.DependsOn)
	if err != nil {
		return Ticket{}, fmt.Errorf("store.createTicket: encoding depends_on: %w", err)
	}

	now := time.Now()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO tickets (id, title, prompt, status, priority, agent, branch, branch_mode, worktree_path, pr_url, tags, depends_on, agent_active, resume_command, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Title, t.Prompt, t.Status, t.Priority, t.Agent, t.Branch, t.BranchMode, t.WorktreePath, t.PRURL, string(tags), string(deps), t.AgentActive, t.ResumeCommand, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return Ticket{}, fmt.Errorf("store.createTicket: %w", err)
	}

	return t, nil
}

func (s *Store) GetTicket(ctx context.Context, id string) (Ticket, error) {
	var r ticketRow
	err := s.db.QueryRowContext(ctx,
		"SELECT id, title, prompt, status, priority, agent, branch, branch_mode, worktree_path, pr_url, tags, depends_on, agent_active, resume_command, created_at, updated_at FROM tickets WHERE id = ?",
		id,
	).Scan(&r.ID, &r.Title, &r.Prompt, &r.Status, &r.Priority, &r.Agent, &r.Branch, &r.BranchMode, &r.WorktreePath, &r.PRURL, &r.Tags, &r.DependsOn, &r.AgentActive, &r.ResumeCommand, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return Ticket{}, fmt.Errorf("store.getTicket %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Ticket{}, fmt.Errorf("store.getTicket %s: %w", id, err)
	}

	ticket, err := r.toTicket()
	if err != nil {
		return Ticket{}, fmt.Errorf("store.getTicket %s: %w", id, err)
	}

	return ticket, nil
}

func (s *Store) ListTickets(ctx context.Context, filters TicketFilters) ([]Ticket, error) {
	query := "SELECT id, title, prompt, status, priority, agent, branch, branch_mode, worktree_path, pr_url, tags, depends_on, agent_active, resume_command, created_at, updated_at FROM tickets WHERE 1=1"
	var args []interface{}

	if filters.Status != "" {
		query += " AND status = ?"
		args = append(args, filters.Status)
	}
	if filters.Agent != "" {
		query += " AND agent = ?"
		args = append(args, filters.Agent)
	}
	if filters.Priority != "" {
		query += " AND priority = ?"
		args = append(args, filters.Priority)
	}
	if filters.Tag != "" {
		query += " AND tags LIKE ?"
		args = append(args, `%"`+filters.Tag+`"%`)
	}
	if filters.From != nil {
		query += " AND created_at >= ?"
		args = append(args, *filters.From)
	}
	if filters.To != nil {
		query += " AND created_at <= ?"
		args = append(args, *filters.To)
	}
	if filters.Search != "" {
		query += " AND (title LIKE ? OR prompt LIKE ?)"
		pattern := "%" + filters.Search + "%"
		args = append(args, pattern, pattern)
	}

	query += " ORDER BY created_at ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store.listTickets: %w", err)
	}
	defer rows.Close()

	var tickets []Ticket
	for rows.Next() {
		var r ticketRow
		if err := rows.Scan(&r.ID, &r.Title, &r.Prompt, &r.Status, &r.Priority, &r.Agent, &r.Branch, &r.BranchMode, &r.WorktreePath, &r.PRURL, &r.Tags, &r.DependsOn, &r.AgentActive, &r.ResumeCommand, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store.listTickets: %w", err)
		}
		ticket, err := r.toTicket()
		if err != nil {
			return nil, fmt.Errorf("store.listTickets: %w", err)
		}
		tickets = append(tickets, ticket)
	}

	return tickets, nil
}

func (s *Store) UpdateTicket(ctx context.Context, t Ticket) (Ticket, error) {
	if !s.isValidStatus(t.Status) {
		return Ticket{}, fmt.Errorf("store.updateTicket: %q: %w", t.Status, ErrInvalidStatus)
	}
	if t.Priority != "" && !validPriorities[t.Priority] {
		return Ticket{}, fmt.Errorf("store.updateTicket: %q: %w", t.Priority, ErrInvalidPriority)
	}

	tags, err := json.Marshal(t.Tags)
	if err != nil {
		return Ticket{}, fmt.Errorf("store.updateTicket: encoding tags: %w", err)
	}
	deps, err := json.Marshal(t.DependsOn)
	if err != nil {
		return Ticket{}, fmt.Errorf("store.updateTicket: encoding depends_on: %w", err)
	}

	t.BranchMode = normalizeBranchMode(t.BranchMode)
	t.UpdatedAt = time.Now()

	result, err := s.db.ExecContext(ctx,
		`UPDATE tickets SET title=?, prompt=?, status=?, priority=?, agent=?, branch=?, branch_mode=?, worktree_path=?, pr_url=?, tags=?, depends_on=?, agent_active=?, resume_command=?, updated_at=? WHERE id=?`,
		t.Title, t.Prompt, t.Status, t.Priority, t.Agent, t.Branch, t.BranchMode, t.WorktreePath, t.PRURL, string(tags), string(deps), t.AgentActive, t.ResumeCommand, t.UpdatedAt, t.ID,
	)
	if err != nil {
		return Ticket{}, fmt.Errorf("store.updateTicket: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return Ticket{}, fmt.Errorf("store.updateTicket %s: %w", t.ID, ErrNotFound)
	}

	return t, nil
}

// PurgeTicketRecords deletes everything recorded about a ticket except the
// ticket itself: a workspace board keeps the sessions and events of tickets
// whose row lives in another repository's board.
func (s *Store) PurgeTicketRecords(ctx context.Context, id string) error {
	for _, table := range []string{"context_carry", "orchestration_events", "proposals", "sessions"} {
		if _, err := s.db.ExecContext(ctx, "DELETE FROM "+table+" WHERE ticket_id = ?", id); err != nil {
			return fmt.Errorf("store.purgeTicketRecords: %s: %w", table, err)
		}
	}
	return nil
}

func (s *Store) DeleteTicket(ctx context.Context, id string) error {
	if err := s.PurgeTicketRecords(ctx, id); err != nil {
		return fmt.Errorf("store.deleteTicket: %w", err)
	}

	result, err := s.db.ExecContext(ctx, "DELETE FROM tickets WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("store.deleteTicket: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("store.deleteTicket %s: %w", id, ErrNotFound)
	}

	return nil
}

func (s *Store) SetAgentActive(ctx context.Context, id string, active bool) error {
	result, err := s.db.ExecContext(ctx,
		"UPDATE tickets SET agent_active = ?, updated_at = ? WHERE id = ?",
		active, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("store.setAgentActive: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("store.setAgentActive %s: %w", id, ErrNotFound)
	}

	return nil
}

func (s *Store) SetResumeCommand(ctx context.Context, id, cmd string) error {
	result, err := s.db.ExecContext(ctx, "UPDATE tickets SET resume_command = ?, updated_at = ? WHERE id = ?",
		cmd, time.Now().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("store.setResumeCommand: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("store.setResumeCommand %s: %w", id, ErrNotFound)
	}

	return nil
}

func (s *Store) GetResumeCommand(ctx context.Context, id string) (string, error) {
	var cmd string
	err := s.db.QueryRowContext(ctx, "SELECT resume_command FROM tickets WHERE id = ?", id).Scan(&cmd)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("get resume command: %w", err)
	}
	return cmd, nil
}

func (s *Store) SetBranch(ctx context.Context, id, branch string) error {
	result, err := s.db.ExecContext(ctx, "UPDATE tickets SET branch = ?, updated_at = ? WHERE id = ?",
		branch, time.Now(), id)
	if err != nil {
		return fmt.Errorf("store.setBranch: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("store.setBranch %s: %w", id, ErrNotFound)
	}

	return nil
}

func (s *Store) SetWorktreePath(ctx context.Context, id, path string) error {
	result, err := s.db.ExecContext(ctx, "UPDATE tickets SET worktree_path = ?, updated_at = ? WHERE id = ?",
		path, time.Now(), id)
	if err != nil {
		return fmt.Errorf("store.setWorktreePath: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("store.setWorktreePath %s: %w", id, ErrNotFound)
	}

	return nil
}

func (s *Store) SetPRURL(ctx context.Context, id, url string) error {
	result, err := s.db.ExecContext(ctx, "UPDATE tickets SET pr_url = ?, updated_at = ? WHERE id = ?",
		url, time.Now(), id)
	if err != nil {
		return fmt.Errorf("store.setPRURL: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("store.setPRURL %s: %w", id, ErrNotFound)
	}

	return nil
}

func (s *Store) MoveStatus(ctx context.Context, id string, status string) error {
	if !s.isValidStatus(status) {
		return fmt.Errorf("store.moveStatus: %q: %w", status, ErrInvalidStatus)
	}

	result, err := s.db.ExecContext(ctx,
		"UPDATE tickets SET status = ?, updated_at = ? WHERE id = ?",
		status, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("store.moveStatus: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("store.moveStatus %s: %w", id, ErrNotFound)
	}

	return nil
}
