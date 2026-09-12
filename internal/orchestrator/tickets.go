package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ayan-de/agent-board/internal/store"
)

var (
	ErrTitleRequired     = errors.New("orchestrator: ticket title is required")
	ErrInvalidBranchMode = errors.New("orchestrator: invalid branch mode")
)

type CreateTicketInput struct {
	Title    string
	Prompt   string
	Priority string
	Agent    string
	Status   string
}

// DefaultBranchName is the branch a ticket gets when nobody named one:
// the prefix followed by the lower-cased ticket id.
func DefaultBranchName(prefix, id string) string {
	if prefix == "" {
		prefix = "agent/"
	}
	return prefix + strings.ToLower(id)
}

func (s *Service) SetBranchPrefix(prefix string) {
	s.mu.Lock()
	s.branchPrefix = prefix
	s.mu.Unlock()
}

func (s *Service) SetTicketPrefix(prefix string) {
	s.store.SetTicketPrefix(prefix)
}

func (s *Service) Statuses() []string {
	return s.store.Statuses()
}

func (s *Service) ProjectInitDate(ctx context.Context) (time.Time, error) {
	return s.store.ProjectInitDate(ctx)
}

func (s *Service) ListTickets(ctx context.Context, f store.TicketFilters) ([]store.Ticket, error) {
	return s.store.ListTickets(ctx, f)
}

func (s *Service) GetTicket(ctx context.Context, id string) (store.Ticket, error) {
	return s.store.GetTicket(ctx, id)
}

// CreateTicket places a new ticket in the board's first column unless the
// caller names another status.
func (s *Service) CreateTicket(ctx context.Context, in CreateTicketInput) (store.Ticket, error) {
	if strings.TrimSpace(in.Title) == "" {
		return store.Ticket{}, ErrTitleRequired
	}
	status := in.Status
	if status == "" {
		statuses := s.store.Statuses()
		if len(statuses) == 0 {
			return store.Ticket{}, fmt.Errorf("orchestrator.createTicket: no statuses configured")
		}
		status = statuses[0]
	}
	t, err := s.store.CreateTicket(ctx, store.Ticket{
		Title:    in.Title,
		Prompt:   in.Prompt,
		Priority: in.Priority,
		Agent:    in.Agent,
		Status:   status,
	})
	if err != nil {
		return store.Ticket{}, fmt.Errorf("orchestrator.createTicket: %w", err)
	}
	return t, nil
}

// UpdateTicket saves editable fields. Status and branch mode are not among
// them: they move through MoveTicketStatus and SetBranchMode, which carry
// the rules those transitions need.
func (s *Service) UpdateTicket(ctx context.Context, t store.Ticket) (store.Ticket, error) {
	current, err := s.store.GetTicket(ctx, t.ID)
	if err != nil {
		return store.Ticket{}, fmt.Errorf("orchestrator.updateTicket: %w", err)
	}
	t.Status = current.Status
	t.BranchMode = current.BranchMode
	t.WorktreePath = current.WorktreePath
	t.PRURL = current.PRURL
	t.AgentActive = current.AgentActive
	t.ResumeCommand = current.ResumeCommand
	updated, err := s.store.UpdateTicket(ctx, t)
	if err != nil {
		return store.Ticket{}, fmt.Errorf("orchestrator.updateTicket: %w", err)
	}
	return updated, nil
}

// SetBranchMode switches a ticket between running in the board's own
// checkout and running in its own worktree. Choosing the current branch
// forgets any worktree branch; choosing a worktree names the default branch
// when the ticket has none, so the caller may rename it before an agent
// starts.
func (s *Service) SetBranchMode(ctx context.Context, ticketID, mode string) (store.Ticket, error) {
	t, err := s.store.GetTicket(ctx, ticketID)
	if err != nil {
		return store.Ticket{}, fmt.Errorf("orchestrator.setBranchMode: %w", err)
	}
	switch mode {
	case store.BranchModeCurrent:
		t.BranchMode = store.BranchModeCurrent
		t.Branch = ""
	case store.BranchModeWorktree:
		t.BranchMode = store.BranchModeWorktree
		if t.Branch == "" {
			s.mu.RLock()
			prefix := s.branchPrefix
			s.mu.RUnlock()
			t.Branch = DefaultBranchName(prefix, t.ID)
		}
	default:
		return store.Ticket{}, fmt.Errorf("%w: %q", ErrInvalidBranchMode, mode)
	}
	updated, err := s.store.UpdateTicket(ctx, t)
	if err != nil {
		return store.Ticket{}, fmt.Errorf("orchestrator.setBranchMode: %w", err)
	}
	return updated, nil
}

func (s *Service) GetProposal(ctx context.Context, id string) (store.Proposal, error) {
	return s.store.GetProposal(ctx, id)
}

func (s *Service) GetActiveProposalForTicket(ctx context.Context, ticketID string) (store.Proposal, error) {
	return s.store.GetActiveProposalForTicket(ctx, ticketID)
}

func (s *Service) ListActiveSessions(ctx context.Context) ([]store.Session, error) {
	return s.store.ListActiveSessions(ctx)
}
