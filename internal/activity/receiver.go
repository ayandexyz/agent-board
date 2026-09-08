package activity

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
)

// Session is one agent's tracked activity.
type Session struct {
	ID       string
	State    State
	ToolName string
	Detail   string
}

// Alert is one "this session needs a human" event, ready to render.
type Alert struct {
	SessionID string
	State     State
	Title     string
	Body      string
}

// Receiver is the loopback HTTP endpoint spawned agents report to.
type Receiver struct {
	// Notify delivers a "needs you" alert. Defaults to notify-send.
	Notify func(title, body string)
	// Label turns a session id into something a human recognises, usually the
	// tmux window it runs in. Defaults to the session id.
	Label func(sessionID string) string
	// events carries needs-input entries to the TUI. Buffered and dropped on
	// overflow: a stalled reader must never block a hook request.
	events chan Alert

	mu       sync.Mutex
	sessions map[string]*Session
	launches map[string]string

	ln  net.Listener
	srv *http.Server
}

func New() *Receiver {
	return &Receiver{
		Notify:   NotifySend,
		sessions: make(map[string]*Session),
		launches: make(map[string]string),
		events:   make(chan Alert, 16),
	}
}

// Start binds 127.0.0.1 on a free port and serves until Close. It returns the
// base URL to hand to spawned agents.
func (r *Receiver) Start() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("activity.Start: %w", err)
	}
	r.ln = ln
	mux := http.NewServeMux()
	mux.HandleFunc("/hooks/", r.handle)
	r.srv = &http.Server{Handler: mux}
	go func() { _ = r.srv.Serve(ln) }()
	return "http://" + ln.Addr().String(), nil
}

func (r *Receiver) Close() error {
	if r.srv == nil {
		return nil
	}
	return r.srv.Close()
}

// Launch records the generation id for a session. Hooks carrying a different
// one belong to an earlier launch and are dropped.
func (r *Receiver) Launch(sessionID, launchID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.launches[sessionID] = launchID
	delete(r.sessions, sessionID)
}

// Forget drops a session, e.g. when its run completes.
func (r *Receiver) Forget(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, sessionID)
	delete(r.launches, sessionID)
}

// StateOf returns the tracked state, or "" if the session has never reported.
func (r *Receiver) StateOf(sessionID string) State {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sessions[sessionID]; ok {
		return s.State
	}
	return ""
}

// Waiting lists the sessions that need a human.
func (r *Receiver) Waiting() []Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Session
	for _, s := range r.sessions {
		if s.State.NeedsInput() {
			out = append(out, *s)
		}
	}
	return out
}

func (r *Receiver) handle(w http.ResponseWriter, req *http.Request) {
	sessionID := strings.TrimPrefix(req.URL.Path, "/hooks/")
	var p payload
	_ = json.NewDecoder(req.Body).Decode(&p)

	w.Header().Set("Content-Type", "application/json")
	// ponytail: PermissionRequest is answered immediately with no decision, so
	// the agent falls through to its own pane picker. Hold the response open
	// once the TUI has a waiting overlay that can answer it.
	defer func() { _, _ = w.Write([]byte("{}")) }()

	if sessionID == "" {
		return
	}
	r.apply(sessionID, req.Header.Get("X-Agentboard-Launch"), p)
}

func (r *Receiver) apply(sessionID, launchID string, p payload) {
	next, ok := p.next()
	if !ok {
		return
	}

	r.mu.Lock()
	if want, known := r.launches[sessionID]; known && launchID != "" && launchID != want {
		r.mu.Unlock()
		return
	}
	s, ok := r.sessions[sessionID]
	if !ok {
		s = &Session{ID: sessionID}
		r.sessions[sessionID] = s
	}
	prev := s.State
	if prev == StateBlocked && !p.clearsBlock() {
		r.mu.Unlock()
		return
	}
	s.State = next
	if p.ToolName != "" {
		s.ToolName = p.ToolName
	}
	if next.NeedsInput() {
		s.Detail = truncate(string(p.ToolInput), 120)
	}
	entered := !prev.NeedsInput() && next.NeedsInput()
	title, body := r.alertText(*s)
	r.mu.Unlock()

	if !entered {
		return
	}
	if r.Notify != nil {
		r.Notify(title, body)
	}
	select {
	case r.events <- Alert{SessionID: s.ID, State: s.State, Title: title, Body: body}:
	default:
	}
}

// Events yields a session each time it starts needing a human. Consumed by the
// TUI to raise an in-app notification alongside the desktop one.
func (r *Receiver) Events() <-chan Alert { return r.events }

// alertText must be called with the lock held.
func (r *Receiver) alertText(s Session) (string, string) {
	name := s.ID
	if r.Label != nil {
		if l := r.Label(s.ID); l != "" {
			name = l
		}
	}
	verb := "needs your answer"
	if s.State == StateBlocked {
		verb = "needs approval"
	}
	body := s.ToolName
	if s.Detail != "" {
		body = strings.TrimSpace(body + " " + s.Detail)
	}
	return fmt.Sprintf("%s %s", name, verb), body
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// NotifySend is the default Linux delivery. Failures are ignored: a missing
// notify-send must never take down the receiver.
func NotifySend(title, body string) {
	_ = exec.Command("notify-send", "-a", "agentboard", title, body).Run()
}
