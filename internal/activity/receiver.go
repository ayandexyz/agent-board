package activity

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Session is one agent's tracked activity.
type Session struct {
	ID       string
	State    State
	ToolName string
	Detail   string
	// Decidable is true while a hook holds this session's permission request
	// open, so Resolve can answer it. A question is never decidable: the
	// answer is typed in the pane.
	Decidable bool
}

// Alert is one "this session needs a human" event, ready to render.
type Alert struct {
	SessionID string
	State     State
	Title     string
	Body      string
	// Decidable is true when a hook is holding its response open for this
	// session, so Resolve can answer it without touching the pane.
	Decidable bool
}

// Receiver is the loopback HTTP endpoint spawned agents report to.
type Receiver struct {
	// Notify delivers a "needs you" alert. Defaults to notify-send.
	Notify func(title, body string)
	// Label turns a session id into something a human recognises, usually the
	// tmux window it runs in. Defaults to the session id.
	Label func(sessionID string) string
	// PermissionTimeout bounds how long a permission hook is held open waiting
	// for a human. On expiry the agent gets no decision and falls back to its
	// own pane picker.
	PermissionTimeout time.Duration

	mu       sync.Mutex
	sessions map[string]*Session
	launches map[string]string
	// decisions records how each session's agent reads a permission decision.
	decisions map[string]DecisionCodec
	pending   map[string]*pending
	// subs are the consumers of the alert stream. Each has its own buffer, so
	// the TUI and a remote client never take alerts from one another.
	subs map[*subscription]struct{}

	ln  net.Listener
	srv *http.Server
}

func New() *Receiver {
	return &Receiver{
		Notify:            NotifySend,
		PermissionTimeout: 10 * time.Minute,
		sessions:          make(map[string]*Session),
		launches:          make(map[string]string),
		decisions:         make(map[string]DecisionCodec),
		pending:           make(map[string]*pending),
		subs:              make(map[*subscription]struct{}),
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
	r.closeSubscriptions()
	if r.srv == nil {
		return nil
	}
	return r.srv.Close()
}

// Launch records the generation id for a session, along with how its agent
// reads a permission decision. Hooks carrying a different launch id belong to
// an earlier launch and are dropped. A nil codec means the agent cannot be
// answered over the wire, and its requests fall through to its own pane picker.
func (r *Receiver) Launch(sessionID, launchID string, decisions DecisionCodec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.launches[sessionID] = launchID
	r.decisions[sessionID] = decisions
	delete(r.sessions, sessionID)
}

// Forget drops a session, e.g. when its run completes.
func (r *Receiver) Forget(sessionID string) {
	r.Resolve(sessionID, "")
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, sessionID)
	delete(r.launches, sessionID)
	delete(r.decisions, sessionID)
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
			c := *s
			_, c.Decidable = r.pending[s.ID]
			out = append(out, c)
		}
	}
	return out
}

func (r *Receiver) handle(w http.ResponseWriter, req *http.Request) {
	sessionID := strings.TrimPrefix(req.URL.Path, "/hooks/")
	var p payload
	_ = json.NewDecoder(req.Body).Decode(&p)

	w.Header().Set("Content-Type", "application/json")
	if sessionID == "" {
		_, _ = w.Write([]byte("{}"))
		return
	}
	launchID := req.Header.Get("X-Agentboard-Launch")
	if p.HookEventName == "PermissionRequest" && !p.asksQuestion() {
		r.servePermission(w, req, sessionID, launchID, p)
		return
	}
	r.apply(sessionID, launchID, p)
	_, _ = w.Write([]byte("{}"))
}

// servePermission holds the hook response open until the board answers or the
// timeout expires. The request is parked before the signal is applied so the
// alert it raises already advertises itself as decidable.
func (r *Receiver) servePermission(w http.ResponseWriter, req *http.Request, sessionID, launchID string, p payload) {
	pend := r.park(sessionID)
	defer r.unpark(sessionID, pend)

	if !r.apply(sessionID, launchID, p) {
		_, _ = w.Write([]byte("{}"))
		return
	}

	var decision string
	select {
	case decision = <-pend.decision:
	case <-time.After(r.PermissionTimeout):
	case <-req.Context().Done():
		return
	}

	r.mu.Lock()
	codec := r.decisions[sessionID]
	r.mu.Unlock()
	_, _ = w.Write(permissionReply(codec, decision))
}

// apply records a signal and reports whether it was accepted.
func (r *Receiver) apply(sessionID, launchID string, p payload) bool {
	next, ok := p.next()
	if !ok {
		return false
	}

	r.mu.Lock()
	if want, known := r.launches[sessionID]; known && launchID != "" && launchID != want {
		r.mu.Unlock()
		return false
	}
	s, ok := r.sessions[sessionID]
	if !ok {
		s = &Session{ID: sessionID}
		r.sessions[sessionID] = s
	}
	prev := s.State
	if prev == StateBlocked && !p.clearsBlock() {
		r.mu.Unlock()
		return false
	}
	s.State = next
	if p.ToolName != "" {
		s.ToolName = p.ToolName
	}
	if next.NeedsInput() {
		s.Detail = toolDetail(s.ToolName, p.ToolInput)
	}
	entered := !prev.NeedsInput() && next.NeedsInput()
	title, body := r.alertText(*s)
	_, decidable := r.pending[sessionID]
	r.mu.Unlock()

	if !entered {
		return true
	}
	if r.Notify != nil {
		r.Notify(title, body)
	}
	r.publish(Alert{SessionID: s.ID, State: s.State, Title: title, Body: body, Decidable: decidable})
	return true
}

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
	body := s.Detail
	if body == "" {
		body = s.ToolName
	}
	return fmt.Sprintf("%s %s", name, verb), body
}

// truncate cuts on runes: a byte-wise cut can split a multi-byte character and
// leave the notification ending in a replacement glyph.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// NotifySend is the default Linux delivery. Failures are ignored: a missing
// notify-send must never take down the receiver.
func NotifySend(title, body string) {
	_ = exec.Command("notify-send", "-a", "agentboard", title, body).Run()
}
