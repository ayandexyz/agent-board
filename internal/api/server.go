// Package api exposes the board over HTTP so a second front end (a phone, a
// script, a browser) can drive the same orchestrator the TUI drives. It binds
// loopback only: anything remote reaches it through an SSH forward or a
// tailnet, never through a port opened to the world.
package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ayan-de/agent-board/internal/activity"
	"github.com/ayan-de/agent-board/internal/orchestrator"
	"github.com/ayan-de/agent-board/internal/store"
)

const DefaultAddr = "127.0.0.1:7878"

var ErrNotLoopback = errors.New("api: address is not loopback")

// Board is what the API needs from the orchestrator. It is the same
// container the TUI talks to, so the two never disagree about a rule.
type Board interface {
	Statuses() []string
	ListTickets(ctx context.Context, f store.TicketFilters) ([]store.Ticket, error)
	GetTicket(ctx context.Context, id string) (store.Ticket, error)
	CreateTicket(ctx context.Context, in orchestrator.CreateTicketInput) (store.Ticket, error)
	UpdateTicket(ctx context.Context, t store.Ticket) (store.Ticket, error)
	SetBranchMode(ctx context.Context, id, mode string) (store.Ticket, error)
	MoveTicketStatus(ctx context.Context, id, status string) error
	CompleteTicket(ctx context.Context, id string) error
	MergeTicket(ctx context.Context, id string, strategy orchestrator.MergeStrategy) (orchestrator.MergeResult, error)
	DeleteTicket(ctx context.Context, id string) error
	StartAdHocRun(ctx context.Context, ticketID, agent, prompt string) (store.Session, error)
	StopSession(ctx context.Context, sessionID string) error
	GetActiveSessions() []*orchestrator.AgentSession
	WindowForSession(sessionID string) string
	SendInput(sessionID, input string) error
	GetPaneContent(sessionID string, lines int) (string, error)
	SubscribeCompletions() (<-chan orchestrator.RunCompletion, func())
}

// Activity is what the API needs from the hook receiver. Nil when no
// receiver is running: sessions then carry no state and nothing is decidable.
type Activity interface {
	StateOf(sessionID string) activity.State
	Waiting() []activity.Session
	Resolve(sessionID, decision string) bool
	Subscribe() (<-chan activity.Alert, func())
}

type Identity struct {
	Name        string
	Version     string
	Project     string
	TmuxSession string
}

type Server struct {
	Addr     string
	Board    Board
	Activity Activity
	Identity Identity

	hub  *eventHub
	once sync.Once
	mux  *http.ServeMux
	srv  *http.Server
	ln   net.Listener
	stop []func()
}

func (s *Server) init() {
	s.once.Do(func() {
		s.hub = newEventHub(eventBuffer)
		s.mux = http.NewServeMux()
		s.routes()
	})
}

// Handler is the router without a listener, for tests and for mounting
// under another server.
func (s *Server) Handler() http.Handler {
	s.init()
	return s.mux
}

// Start binds the address, refuses anything that is not loopback, and begins
// forwarding run completions and activity alerts onto the event stream.
// It returns the base URL.
func (s *Server) Start() (string, error) {
	s.init()
	addr := s.Addr
	if addr == "" {
		addr = DefaultAddr
	}
	if err := requireLoopback(addr); err != nil {
		return "", err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("api.start: %w", err)
	}
	s.ln = ln
	s.srv = &http.Server{Handler: s.mux, ReadHeaderTimeout: 10 * time.Second}
	go s.srv.Serve(ln)
	s.forward()
	return "http://" + ln.Addr().String(), nil
}

func (s *Server) forward() {
	if s.Board != nil {
		completions, cancel := s.Board.SubscribeCompletions()
		s.stop = append(s.stop, cancel)
		go func() {
			for c := range completions {
				s.hub.publish("run.completed", c)
			}
		}()
	}
	if s.Activity != nil {
		alerts, cancel := s.Activity.Subscribe()
		s.stop = append(s.stop, cancel)
		go func() {
			for a := range alerts {
				s.hub.publish("activity.alert", a)
			}
		}()
	}
}

func (s *Server) Close() error {
	for _, stop := range s.stop {
		stop()
	}
	if s.hub != nil {
		s.hub.close()
	}
	if s.srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.srv.Shutdown(ctx)
}

func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("api.start: %w", err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%w: %s", ErrNotLoopback, addr)
	}
	return nil
}
