package orchestrator

import (
	"context"

	"github.com/ayan-de/agent-board/internal/llm"
	"github.com/ayan-de/agent-board/internal/store"
)

type CreateProposalInput struct {
	TicketID string
}

type ApplyRunOutcomeInput struct {
	TicketID      string
	Outcome       string
	ResumeCommand string
}

type FinishRunInput struct {
	TicketID      string
	SessionID     string
	Outcome       string
	Summary       string
	ResumeCommand string
}

type RunRequest struct {
	TicketID   string
	SessionID  string
	Agent      string
	Prompt     string
	WorkDir    string
	Reporter   func(string)
	Target     string
	OnComplete func(outcome, summary, resumeCommand string)
	// Env is exported into the agent's tmux window as KEY=VALUE pairs. It
	// carries the session identity an agent's hooks report back with.
	Env []string
}

type WorktreeInfo struct {
	Path    string
	Branch  string
	Base    string
	Created bool
}

type Worktrees interface {
	Ensure(ctx context.Context, t store.Ticket) (WorktreeInfo, error)
	Remove(ctx context.Context, path string, force bool) error
	IsDirty(ctx context.Context, path string) (bool, error)
}

type RunHandle struct {
	Outcome string
	Summary string
}

type RunCompletion struct {
	TicketID      string
	SessionID     string
	Outcome       string
	Summary       string
	ResumeCommand string
}

type LLMClient interface {
	GenerateProposal(ctx context.Context, input llm.ProposalPrompt) (llm.ProposalDraft, error)
	SummarizeContext(ctx context.Context, input llm.SummaryInput) (string, error)
}

type Runner interface {
	Start(ctx context.Context, req RunRequest) (RunHandle, error)
}

type AgentRunner interface {
	Start(ctx context.Context, req RunRequest) (RunHandle, error)
	GetPaneID(sessionID string) (string, bool)
}

type Store interface {
	GetTicket(ctx context.Context, id string) (store.Ticket, error)
	CreateProposal(ctx context.Context, p store.Proposal) (store.Proposal, error)
	GetProposal(ctx context.Context, id string) (store.Proposal, error)
	UpdateProposalStatus(ctx context.Context, id, status string) error
	GetContextCarry(ctx context.Context, ticketID string) (store.ContextCarry, error)
	UpsertContextCarry(ctx context.Context, cc store.ContextCarry) error
	CreateSession(ctx context.Context, sess store.Session) (store.Session, error)
	EndSession(ctx context.Context, id, status string) error
	HasActiveSession(ctx context.Context, ticketID string) bool
	SetAgentActive(ctx context.Context, id string, active bool) error
	SetResumeCommand(ctx context.Context, id, cmd string) error
	SetBranch(ctx context.Context, id, branch string) error
	SetWorktreePath(ctx context.Context, id, path string) error
	SetPRURL(ctx context.Context, id, url string) error
	MoveStatus(ctx context.Context, id, status string) error
	DeleteTicket(ctx context.Context, id string) error
	CreateEvent(ctx context.Context, e store.Event) (store.Event, error)
}
type ContextCarryProvider interface {
	LoadContext(ctx context.Context, ticketID string) (string, error)
	SaveContext(ctx context.Context, ticketID, outcome string) error
}
