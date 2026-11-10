// Package workspace lets one board show tickets from several repositories.
// A ticket has exactly one home, its repository's own board, so opening
// AgentBoard inside that repository shows the same ticket with no copy to
// keep in step. The workspace's own board keeps what belongs to the process
// running the agents: sessions, events, proposals and context.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/ayandexyz/agent-board/internal/orchestrator"
	"github.com/ayandexyz/agent-board/internal/store"
)

var (
	ErrNotARepo      = errors.New("workspace: not a git repository")
	ErrPrefixClash   = errors.New("workspace: ticket prefix already used by another board")
	ErrColumnsDiffer = errors.New("workspace: board columns differ")
)

// TicketStore is what the workspace needs from a linked repository's board.
type TicketStore interface {
	GetTicket(ctx context.Context, id string) (store.Ticket, error)
	ListTickets(ctx context.Context, f store.TicketFilters) ([]store.Ticket, error)
	CreateTicket(ctx context.Context, t store.Ticket) (store.Ticket, error)
	UpdateTicket(ctx context.Context, t store.Ticket) (store.Ticket, error)
	DeleteTicket(ctx context.Context, id string) error
	SetAgentActive(ctx context.Context, id string, active bool) error
	SetResumeCommand(ctx context.Context, id, cmd string) error
	SetBranch(ctx context.Context, id, branch string) error
	SetWorktreePath(ctx context.Context, id, path string) error
	SetPRURL(ctx context.Context, id, url string) error
	MoveStatus(ctx context.Context, id, status string) error
	Statuses() []string
	TicketPrefix() string
	Close() error
}

// Home is the board the workspace was opened on.
type Home interface {
	orchestrator.Store
	TicketPrefix() string
	LinkRepo(ctx context.Context, r store.LinkedRepo) error
	ListLinkedRepos(ctx context.Context) ([]store.LinkedRepo, error)
	PurgeTicketRecords(ctx context.Context, id string) error
	Close() error
}

// Board is a repository's own board, opened for the workspace.
type Board struct {
	Root  string
	Key   string
	Store TicketStore
}

// Opener resolves any path inside a repository to that repository's board.
// It returns ErrNotARepo for a path outside git.
type Opener func(ctx context.Context, path string) (Board, error)

type Workspace struct {
	Home
	homeRoot string
	open     Opener

	mu       sync.RWMutex
	children []Board
}

var _ orchestrator.Store = (*Workspace)(nil)

// New wraps home. homeRoot is the repository the workspace itself sits in,
// or "" outside git; a ticket naming it stays on the home board.
func New(home Home, homeRoot string, open Opener) *Workspace {
	return &Workspace{Home: home, homeRoot: homeRoot, open: open}
}

// Restore reopens every linked repository. A repository that can no longer be
// opened is reported and skipped, so one moved checkout does not hide the
// tickets of all the others.
func (w *Workspace) Restore(ctx context.Context) error {
	linked, err := w.Home.ListLinkedRepos(ctx)
	if err != nil {
		return fmt.Errorf("workspace.restore: %w", err)
	}
	var errs []error
	for _, r := range linked {
		if _, err := w.Link(ctx, r.Path); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Repos lists the roots of the linked repositories in the order they were
// linked.
func (w *Workspace) Repos() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]string, len(w.children))
	for i, c := range w.children {
		out[i] = c.Root
	}
	return out
}

// Link opens the board of the repository containing path and returns its
// root, or "" when path is the workspace's own repository.
func (w *Workspace) Link(ctx context.Context, path string) (string, error) {
	w.mu.RLock()
	for _, c := range w.children {
		if c.Root == path {
			w.mu.RUnlock()
			return c.Root, nil
		}
	}
	w.mu.RUnlock()

	b, err := w.open(ctx, path)
	if err != nil {
		return "", fmt.Errorf("workspace.link %s: %w", path, err)
	}
	if b.Root == w.homeRoot {
		return "", nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	for _, c := range w.children {
		if c.Root == b.Root {
			return c.Root, nil
		}
	}
	if err := w.compatible(b); err != nil {
		_ = b.Store.Close()
		return "", fmt.Errorf("workspace.link %s: %w", b.Root, err)
	}
	if err := w.Home.LinkRepo(ctx, store.LinkedRepo{Path: b.Root, Key: b.Key}); err != nil {
		_ = b.Store.Close()
		return "", fmt.Errorf("workspace.link %s: %w", b.Root, err)
	}
	w.children = append(w.children, b)
	return b.Root, nil
}

// compatible refuses a board whose ticket ids could be confused with another
// board's, since ids are how a ticket is routed home, and one whose columns
// the workspace could not show.
func (w *Workspace) compatible(b Board) error {
	prefix := b.Store.TicketPrefix()
	taken := map[string]string{w.Home.TicketPrefix(): "this board"}
	for _, c := range w.children {
		taken[c.Store.TicketPrefix()] = c.Root
	}
	for p, owner := range taken {
		if strings.HasPrefix(prefix, p) || strings.HasPrefix(p, prefix) {
			return fmt.Errorf("%w: %q is used by %s; set [board] prefix in this repository's config", ErrPrefixClash, prefix, owner)
		}
	}
	if !slices.Equal(b.Store.Statuses(), w.Home.Statuses()) {
		return fmt.Errorf("%w: %v, this board has %v", ErrColumnsDiffer, b.Store.Statuses(), w.Home.Statuses())
	}
	return nil
}

func (w *Workspace) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var errs []error
	for _, c := range w.children {
		errs = append(errs, c.Store.Close())
	}
	w.children = nil
	return errors.Join(errs...)
}

// owner returns the board a ticket id belongs to and that board's root, ""
// for the home board.
func (w *Workspace) owner(id string) (TicketStore, string) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	for _, c := range w.children {
		if strings.HasPrefix(id, c.Store.TicketPrefix()) {
			return c.Store, c.Root
		}
	}
	return w.Home, ""
}

func withRepo(t store.Ticket, root string) store.Ticket {
	t.Repo = root
	return t
}

func (w *Workspace) GetTicket(ctx context.Context, id string) (store.Ticket, error) {
	s, root := w.owner(id)
	t, err := s.GetTicket(ctx, id)
	if err != nil {
		return store.Ticket{}, err
	}
	return withRepo(t, root), nil
}

func (w *Workspace) ListTickets(ctx context.Context, f store.TicketFilters) ([]store.Ticket, error) {
	out, err := w.Home.ListTickets(ctx, f)
	if err != nil {
		return nil, err
	}
	w.mu.RLock()
	children := slices.Clone(w.children)
	w.mu.RUnlock()
	for _, c := range children {
		tickets, err := c.Store.ListTickets(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("workspace.listTickets %s: %w", c.Root, err)
		}
		for _, t := range tickets {
			out = append(out, withRepo(t, c.Root))
		}
	}
	if len(children) > 0 {
		sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	}
	return out, nil
}

func (w *Workspace) CreateTicket(ctx context.Context, t store.Ticket) (store.Ticket, error) {
	root := ""
	if t.Repo != "" && t.Repo != w.homeRoot {
		r, err := w.Link(ctx, t.Repo)
		if err != nil {
			return store.Ticket{}, err
		}
		root = r
	}
	t.Repo = ""
	created, err := w.board(root).CreateTicket(ctx, t)
	if err != nil {
		return store.Ticket{}, err
	}
	return withRepo(created, root), nil
}

// board returns the linked board at root, or the home board.
func (w *Workspace) board(root string) TicketStore {
	w.mu.RLock()
	defer w.mu.RUnlock()
	for _, c := range w.children {
		if c.Root == root {
			return c.Store
		}
	}
	return w.Home
}

func (w *Workspace) UpdateTicket(ctx context.Context, t store.Ticket) (store.Ticket, error) {
	s, root := w.owner(t.ID)
	t.Repo = ""
	updated, err := s.UpdateTicket(ctx, t)
	if err != nil {
		return store.Ticket{}, err
	}
	return withRepo(updated, root), nil
}

func (w *Workspace) DeleteTicket(ctx context.Context, id string) error {
	s, root := w.owner(id)
	if err := s.DeleteTicket(ctx, id); err != nil {
		return err
	}
	if root != "" {
		return w.Home.PurgeTicketRecords(ctx, id)
	}
	return nil
}

func (w *Workspace) SetAgentActive(ctx context.Context, id string, active bool) error {
	s, _ := w.owner(id)
	return s.SetAgentActive(ctx, id, active)
}

func (w *Workspace) SetResumeCommand(ctx context.Context, id, cmd string) error {
	s, _ := w.owner(id)
	return s.SetResumeCommand(ctx, id, cmd)
}

func (w *Workspace) SetBranch(ctx context.Context, id, branch string) error {
	s, _ := w.owner(id)
	return s.SetBranch(ctx, id, branch)
}

func (w *Workspace) SetWorktreePath(ctx context.Context, id, path string) error {
	s, _ := w.owner(id)
	return s.SetWorktreePath(ctx, id, path)
}

func (w *Workspace) SetPRURL(ctx context.Context, id, url string) error {
	s, _ := w.owner(id)
	return s.SetPRURL(ctx, id, url)
}

func (w *Workspace) MoveStatus(ctx context.Context, id, status string) error {
	s, _ := w.owner(id)
	return s.MoveStatus(ctx, id, status)
}
