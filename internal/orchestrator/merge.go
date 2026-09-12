package orchestrator

import (
	"context"
	"fmt"

	"github.com/ayan-de/agent-board/internal/git"
	"github.com/ayan-de/agent-board/internal/store"
)

type MergeStrategy string

const (
	MergeStrategyLocal MergeStrategy = "local"
	MergeStrategyPR    MergeStrategy = "pr"
)

type MergeOutcome string

const (
	MergeOutcomeMerged    MergeOutcome = "merged"
	MergeOutcomeConflict  MergeOutcome = "conflict"
	MergeOutcomePRCreated MergeOutcome = "pr_created"
	MergeOutcomeDirty     MergeOutcome = "dirty"
	MergeOutcomeError     MergeOutcome = "error"
)

type MergeResult struct {
	Outcome MergeOutcome
	Branch  string
	PRURL   string
	Detail  string
}

type MergeRequiredError struct {
	TicketID string
	Strategy MergeStrategy
	Branch   string
	PRURL    string
}

func (e *MergeRequiredError) Error() string {
	return fmt.Sprintf("ticket %s requires a merge before done (strategy %s)", e.TicketID, e.Strategy)
}

type Merger interface {
	MergeLocal(ctx context.Context, t store.Ticket) (MergeResult, error)
	CreatePR(ctx context.Context, t store.Ticket, title, body string) (MergeResult, error)
	PRState(ctx context.Context, t store.Ticket) (git.PRInfo, error)
	Cleanup(ctx context.Context, t store.Ticket, forceBranch bool) error
}

func (s *Service) SetMergeConfig(defaultStrategy MergeStrategy) {
	s.mergeStrategy = defaultStrategy
}

func (s *Service) effectiveStrategy(strategy MergeStrategy) MergeStrategy {
	if strategy != "" {
		return strategy
	}
	if s.mergeStrategy != "" {
		return s.mergeStrategy
	}
	return MergeStrategyLocal
}

func (s *Service) MergeTicket(ctx context.Context, ticketID string, strategy MergeStrategy) (MergeResult, error) {
	if s.merger == nil {
		return MergeResult{}, fmt.Errorf("orchestrator.mergeTicket: git integration not configured")
	}
	if err := s.reserve(ticketID, "merging", true); err != nil {
		return MergeResult{}, err
	}
	defer s.release(ticketID)
	if s.store.HasActiveSession(ctx, ticketID) {
		return MergeResult{}, fmt.Errorf("orchestrator.mergeTicket: agent session active on %s", ticketID)
	}

	ticket, err := s.store.GetTicket(ctx, ticketID)
	if err != nil {
		return MergeResult{}, err
	}

	if ticket.WorktreePath != "" && s.worktrees != nil {
		dirty, err := s.worktrees.IsDirty(ctx, ticket.WorktreePath)
		if err != nil {
			return MergeResult{}, fmt.Errorf("orchestrator.mergeTicket: %w", err)
		}
		if dirty {
			return MergeResult{Outcome: MergeOutcomeDirty, Detail: "worktree has uncommitted changes"}, nil
		}
	}

	resolved := s.effectiveStrategy(strategy)
	_, _ = s.store.CreateEvent(ctx, store.Event{
		TicketID: ticketID,
		Kind:     "merge.started",
		Payload:  string(resolved),
	})

	switch resolved {
	case MergeStrategyLocal:
		res, err := s.merger.MergeLocal(ctx, ticket)
		if err != nil {
			return MergeResult{Outcome: MergeOutcomeError, Detail: err.Error()}, err
		}
		switch res.Outcome {
		case MergeOutcomeMerged:
			if err := s.finalizeMerge(ctx, ticket, false); err != nil {
				return res, err
			}
		case MergeOutcomeConflict:
			_, _ = s.store.CreateEvent(ctx, store.Event{
				TicketID: ticketID,
				Kind:     "merge.conflict",
				Payload:  res.Detail,
			})
		}
		return res, nil

	case MergeStrategyPR:
		title := fmt.Sprintf("%s: %s", ticket.ID, ticket.Title)
		body := ticket.Prompt
		if cc, err := s.store.GetContextCarry(ctx, ticketID); err == nil && cc.Summary != "" {
			body = body + "\n\n" + cc.Summary
		}
		res, err := s.merger.CreatePR(ctx, ticket, title, body)
		if err != nil {
			return MergeResult{Outcome: MergeOutcomeError, Detail: err.Error()}, err
		}
		if res.PRURL != "" {
			_ = s.store.SetPRURL(ctx, ticketID, res.PRURL)
			_, _ = s.store.CreateEvent(ctx, store.Event{
				TicketID: ticketID,
				Kind:     "pr.created",
				Payload:  res.PRURL,
			})
		}
		return res, nil

	default:
		return MergeResult{}, fmt.Errorf("orchestrator.mergeTicket: unknown strategy %q", resolved)
	}
}

// finalizeMerge cleans up first and moves the ticket only once the worktree
// is gone. A cleanup that fails leaves the ticket in review with its path
// intact, so the next merge attempt retries the cleanup instead of the
// worktree being orphaned behind a done ticket.
func (s *Service) finalizeMerge(ctx context.Context, ticket store.Ticket, forceBranch bool) error {
	if err := s.merger.Cleanup(ctx, ticket, forceBranch); err != nil {
		_, _ = s.store.CreateEvent(ctx, store.Event{
			TicketID: ticket.ID,
			Kind:     "merge.cleanup_failed",
			Payload:  err.Error(),
		})
		return fmt.Errorf("orchestrator.finalizeMerge: merged, but worktree cleanup failed and %s stays in review (merge again to retry): %w", ticket.ID, err)
	}
	_ = s.store.SetWorktreePath(ctx, ticket.ID, "")
	if err := s.store.MoveStatus(ctx, ticket.ID, "done"); err != nil {
		return err
	}
	_, _ = s.store.CreateEvent(ctx, store.Event{
		TicketID: ticket.ID,
		Kind:     "merge.completed",
		Payload:  ticket.Branch,
	})
	return nil
}

func (s *Service) MoveTicketStatus(ctx context.Context, ticketID, newStatus string) error {
	ticket, err := s.store.GetTicket(ctx, ticketID)
	if err != nil {
		return err
	}

	if ticket.Status == "review" && newStatus == "done" && s.merger != nil && ticket.UsesWorktree() {
		hasWork := ticket.WorktreePath != "" || ticket.Branch != "" || ticket.PRURL != ""
		if hasWork {
			if ticket.PRURL != "" {
				info, err := s.merger.PRState(ctx, ticket)
				if err == nil && info.State == "MERGED" {
					if err := s.reserve(ticketID, "merging", true); err != nil {
						return err
					}
					defer s.release(ticketID)
					return s.finalizeMerge(ctx, ticket, true)
				}
				return &MergeRequiredError{
					TicketID: ticketID,
					Strategy: MergeStrategyPR,
					Branch:   ticket.Branch,
					PRURL:    ticket.PRURL,
				}
			}
			return &MergeRequiredError{
				TicketID: ticketID,
				Strategy: s.effectiveStrategy(""),
				Branch:   ticket.Branch,
			}
		}
	}

	if err := s.store.MoveStatus(ctx, ticketID, newStatus); err != nil {
		return err
	}
	_, _ = s.store.CreateEvent(ctx, store.Event{
		TicketID: ticketID,
		Kind:     "status.changed",
		Payload:  newStatus,
	})
	return nil
}

func (s *Service) DeleteTicket(ctx context.Context, ticketID string) error {
	ticket, err := s.store.GetTicket(ctx, ticketID)
	if err != nil {
		return err
	}
	if err := s.reserve(ticketID, "deleting", true); err != nil {
		return err
	}
	defer s.release(ticketID)
	if ticket.WorktreePath != "" && s.worktrees != nil {
		if err := s.worktrees.Remove(ctx, ticket.WorktreePath, false); err != nil {
			return fmt.Errorf("orchestrator.deleteTicket: %w", err)
		}
	}
	return s.store.DeleteTicket(ctx, ticketID)
}
