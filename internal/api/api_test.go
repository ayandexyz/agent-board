package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ayandexyz/agent-board/internal/activity"
	"github.com/ayandexyz/agent-board/internal/api"
	"github.com/ayandexyz/agent-board/internal/apitypes"
	"github.com/ayandexyz/agent-board/internal/orchestrator"
	"github.com/ayandexyz/agent-board/internal/store"
)

type fakeBoard struct {
	tickets     map[string]store.Ticket
	sessions    []*orchestrator.AgentSession
	completions chan orchestrator.RunCompletion

	lastStatus      string
	lastStart       [3]string
	lastInput       [2]string
	lastStopped     string
	completeErr     error
	mergeResult     orchestrator.MergeResult
	startErr        error
	inputErr        error
	deleted         string
	branchModeCalls []string
	diff            orchestrator.TicketDiff
	diffErr         error
}

func newFakeBoard() *fakeBoard {
	return &fakeBoard{
		tickets:     map[string]store.Ticket{},
		completions: make(chan orchestrator.RunCompletion, 8),
	}
}

func (f *fakeBoard) Statuses() []string { return []string{"backlog", "in_progress", "review", "done"} }
func (f *fakeBoard) ListTickets(_ context.Context, fl store.TicketFilters) ([]store.Ticket, error) {
	var out []store.Ticket
	for _, t := range f.tickets {
		if fl.Status != "" && t.Status != fl.Status {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}
func (f *fakeBoard) GetTicket(_ context.Context, id string) (store.Ticket, error) {
	t, ok := f.tickets[id]
	if !ok {
		return store.Ticket{}, fmt.Errorf("get %s: %w", id, store.ErrNotFound)
	}
	return t, nil
}
func (f *fakeBoard) CreateTicket(_ context.Context, in orchestrator.CreateTicketInput) (store.Ticket, error) {
	if in.Title == "" {
		return store.Ticket{}, orchestrator.ErrTitleRequired
	}
	t := store.Ticket{ID: fmt.Sprintf("AGT-%02d", len(f.tickets)+1), Title: in.Title, Prompt: in.Prompt, Status: "backlog", Priority: "medium", Agent: in.Agent, BranchMode: "worktree"}
	f.tickets[t.ID] = t
	return t, nil
}
func (f *fakeBoard) UpdateTicket(_ context.Context, t store.Ticket) (store.Ticket, error) {
	cur, ok := f.tickets[t.ID]
	if !ok {
		return store.Ticket{}, store.ErrNotFound
	}
	t.Status = cur.Status
	f.tickets[t.ID] = t
	return t, nil
}
func (f *fakeBoard) SetBranchMode(_ context.Context, id, mode string) (store.Ticket, error) {
	f.branchModeCalls = append(f.branchModeCalls, mode)
	t, ok := f.tickets[id]
	if !ok {
		return store.Ticket{}, store.ErrNotFound
	}
	if mode != "current" && mode != "worktree" {
		return store.Ticket{}, orchestrator.ErrInvalidBranchMode
	}
	t.BranchMode = mode
	f.tickets[id] = t
	return t, nil
}
func (f *fakeBoard) MoveTicketStatus(_ context.Context, id, status string) error {
	t, ok := f.tickets[id]
	if !ok {
		return store.ErrNotFound
	}
	t.Status = status
	f.tickets[id] = t
	f.lastStatus = status
	return nil
}
func (f *fakeBoard) CompleteTicket(_ context.Context, id string) error {
	if f.completeErr != nil {
		return f.completeErr
	}
	return f.MoveTicketStatus(context.Background(), id, "done")
}
func (f *fakeBoard) MergeTicket(_ context.Context, _ string, _ orchestrator.MergeStrategy) (orchestrator.MergeResult, error) {
	return f.mergeResult, nil
}
func (f *fakeBoard) DeleteTicket(_ context.Context, id string) error {
	if _, ok := f.tickets[id]; !ok {
		return store.ErrNotFound
	}
	delete(f.tickets, id)
	f.deleted = id
	return nil
}
func (f *fakeBoard) StartAdHocRun(_ context.Context, ticketID, agent, prompt string) (store.Session, error) {
	if f.startErr != nil {
		return store.Session{}, f.startErr
	}
	f.lastStart = [3]string{ticketID, agent, prompt}
	f.sessions = append(f.sessions, &orchestrator.AgentSession{SessionID: "SES-01", TicketID: ticketID})
	return store.Session{ID: "SES-01", TicketID: ticketID, Agent: agent, Status: "running", StartedAt: time.Now()}, nil
}
func (f *fakeBoard) StopSession(_ context.Context, id string) error  { f.lastStopped = id; return nil }
func (f *fakeBoard) GetActiveSessions() []*orchestrator.AgentSession { return f.sessions }
func (f *fakeBoard) WindowForSession(id string) string {
	for _, s := range f.sessions {
		if s.SessionID == id {
			return "agent-" + id
		}
	}
	return ""
}
func (f *fakeBoard) SendInput(id, in string) error {
	if f.inputErr != nil {
		return f.inputErr
	}
	f.lastInput = [2]string{id, in}
	return nil
}
func (f *fakeBoard) GetPaneContent(id string, lines int) (string, error) {
	return fmt.Sprintf("screen of %s (%d)", id, lines), nil
}
func (f *fakeBoard) SubscribeCompletions() (<-chan orchestrator.RunCompletion, func()) {
	return f.completions, func() {}
}

type fakeActivity struct {
	states   map[string]activity.State
	waiting  []activity.Session
	resolved [2]string
	alerts   chan activity.Alert
}

func (f *fakeActivity) StateOf(id string) activity.State { return f.states[id] }
func (f *fakeActivity) Waiting() []activity.Session      { return f.waiting }
func (f *fakeActivity) Resolve(id, decision string) bool {
	for _, w := range f.waiting {
		if w.ID == id && w.Decidable {
			f.resolved = [2]string{id, decision}
			return true
		}
	}
	return false
}
func (f *fakeActivity) Subscribe() (<-chan activity.Alert, func()) { return f.alerts, func() {} }

func newTestServer(t *testing.T, b *fakeBoard, a *fakeActivity) (*httptest.Server, *api.Server) {
	t.Helper()
	s := &api.Server{Board: b, Identity: api.Identity{Name: "agentboard", Version: "test", Project: "demo", TmuxSession: "demo"}}
	if a != nil {
		s.Activity = a
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, s
}

func do(t *testing.T, ts *httptest.Server, method, path string, body interface{}) (*http.Response, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, ts.URL+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return resp, out.Bytes()
}

func TestIdentity(t *testing.T) {
	ts, _ := newTestServer(t, newFakeBoard(), nil)
	resp, body := do(t, ts, "GET", "/api/v1/identity", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var id apitypes.Identity
	_ = json.Unmarshal(body, &id)
	if id.Project != "demo" || id.API != "v1" || id.TmuxSession != "demo" {
		t.Errorf("identity = %+v", id)
	}
}

func TestCreateAndListTickets(t *testing.T) {
	b := newFakeBoard()
	ts, _ := newTestServer(t, b, nil)

	resp, body := do(t, ts, "POST", "/api/v1/tickets", apitypes.CreateTicket{Title: "Add login", Agent: "claude"})
	if resp.StatusCode != 201 {
		t.Fatalf("create status %d: %s", resp.StatusCode, body)
	}
	var created apitypes.Ticket
	_ = json.Unmarshal(body, &created)
	if created.ID != "AGT-01" || created.Status != "backlog" || created.Tags == nil {
		t.Errorf("created = %+v", created)
	}

	resp, body = do(t, ts, "POST", "/api/v1/tickets", apitypes.CreateTicket{})
	if resp.StatusCode != 400 {
		t.Errorf("empty title status %d: %s", resp.StatusCode, body)
	}

	resp, body = do(t, ts, "GET", "/api/v1/tickets?status=backlog", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("list status %d", resp.StatusCode)
	}
	var list []apitypes.Ticket
	_ = json.Unmarshal(body, &list)
	if len(list) != 1 || list[0].ID != "AGT-01" {
		t.Errorf("list = %+v", list)
	}
}

func TestBoardGroupsByStatusAndCarriesActivity(t *testing.T) {
	b := newFakeBoard()
	b.tickets["AGT-01"] = store.Ticket{ID: "AGT-01", Title: "a", Status: "in_progress"}
	b.tickets["AGT-02"] = store.Ticket{ID: "AGT-02", Title: "b", Status: "backlog"}
	b.sessions = []*orchestrator.AgentSession{{SessionID: "SES-01", TicketID: "AGT-01", Agent: "claude"}}
	a := &fakeActivity{
		states:  map[string]activity.State{"SES-01": activity.StateBlocked},
		waiting: []activity.Session{{ID: "SES-01", State: activity.StateBlocked, ToolName: "Bash", Decidable: true}},
	}
	ts, _ := newTestServer(t, b, a)

	resp, body := do(t, ts, "GET", "/api/v1/board", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var board apitypes.Board
	_ = json.Unmarshal(body, &board)
	if len(board.Columns) != 4 || board.Columns[0].Status != "backlog" || board.Waiting != 1 {
		t.Fatalf("board = %+v", board)
	}
	inProg := board.Columns[1]
	if len(inProg.Tickets) != 1 || inProg.Tickets[0].Activity != "blocked" || inProg.Tickets[0].SessionID != "SES-01" {
		t.Errorf("in_progress column = %+v", inProg)
	}
	if len(board.Columns[3].Tickets) != 0 {
		t.Errorf("done column should be an empty list, got %+v", board.Columns[3].Tickets)
	}
}

func TestPatchTicketIsPartial(t *testing.T) {
	b := newFakeBoard()
	b.tickets["AGT-01"] = store.Ticket{ID: "AGT-01", Title: "old", Prompt: "keep me", Status: "backlog"}
	ts, _ := newTestServer(t, b, nil)

	resp, body := do(t, ts, "PATCH", "/api/v1/tickets/AGT-01", map[string]string{"title": "new"})
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	got := b.tickets["AGT-01"]
	if got.Title != "new" || got.Prompt != "keep me" {
		t.Errorf("ticket = %+v", got)
	}
	resp, _ = do(t, ts, "PATCH", "/api/v1/tickets/NOPE", map[string]string{"title": "x"})
	if resp.StatusCode != 404 {
		t.Errorf("missing ticket status %d", resp.StatusCode)
	}
}

func TestStatusBranchModeAndDelete(t *testing.T) {
	b := newFakeBoard()
	b.tickets["AGT-01"] = store.Ticket{ID: "AGT-01", Title: "t", Status: "backlog"}
	ts, _ := newTestServer(t, b, nil)

	resp, body := do(t, ts, "POST", "/api/v1/tickets/AGT-01/status", apitypes.SetStatus{Status: "review"})
	if resp.StatusCode != 200 || b.lastStatus != "review" {
		t.Errorf("status: %d %s (moved to %q)", resp.StatusCode, body, b.lastStatus)
	}
	resp, _ = do(t, ts, "POST", "/api/v1/tickets/AGT-01/branch-mode", apitypes.SetBranchMode{Mode: "current"})
	if resp.StatusCode != 200 || b.tickets["AGT-01"].BranchMode != "current" {
		t.Errorf("branch-mode status %d", resp.StatusCode)
	}
	resp, _ = do(t, ts, "POST", "/api/v1/tickets/AGT-01/branch-mode", apitypes.SetBranchMode{Mode: "bogus"})
	if resp.StatusCode != 400 {
		t.Errorf("bogus mode status %d", resp.StatusCode)
	}
	resp, _ = do(t, ts, "DELETE", "/api/v1/tickets/AGT-01", nil)
	if resp.StatusCode != 204 || b.deleted != "AGT-01" {
		t.Errorf("delete status %d", resp.StatusCode)
	}
}

func TestStartUsesTicketAgentAndPromptByDefault(t *testing.T) {
	b := newFakeBoard()
	b.tickets["AGT-01"] = store.Ticket{ID: "AGT-01", Title: "t", Prompt: "do it", Agent: "opencode", Status: "backlog"}
	b.tickets["AGT-02"] = store.Ticket{ID: "AGT-02", Title: "t", Status: "backlog"}
	ts, _ := newTestServer(t, b, nil)

	resp, body := do(t, ts, "POST", "/api/v1/tickets/AGT-01/start", nil)
	if resp.StatusCode != 201 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if b.lastStart != [3]string{"AGT-01", "opencode", "do it"} {
		t.Errorf("start = %v", b.lastStart)
	}
	var sess apitypes.Session
	_ = json.Unmarshal(body, &sess)
	if sess.Window != "agent-SES-01" {
		t.Errorf("session = %+v", sess)
	}

	resp, _ = do(t, ts, "POST", "/api/v1/tickets/AGT-02/start", nil)
	if resp.StatusCode != 400 {
		t.Errorf("no agent status %d", resp.StatusCode)
	}
	resp, _ = do(t, ts, "POST", "/api/v1/tickets/AGT-02/start", apitypes.StartRun{Agent: "claude"})
	if resp.StatusCode != 201 || b.lastStart[1] != "claude" {
		t.Errorf("explicit agent status %d, start %v", resp.StatusCode, b.lastStart)
	}

	b.startErr = orchestrator.ErrTicketBusy
	resp, _ = do(t, ts, "POST", "/api/v1/tickets/AGT-01/start", nil)
	if resp.StatusCode != 409 {
		t.Errorf("busy status %d", resp.StatusCode)
	}
}

func TestCompleteReportsMergeRequired(t *testing.T) {
	b := newFakeBoard()
	b.tickets["AGT-01"] = store.Ticket{ID: "AGT-01", Title: "t", Status: "review"}
	b.completeErr = &orchestrator.MergeRequiredError{TicketID: "AGT-01", Branch: "agent/agt-01", Strategy: orchestrator.MergeStrategyLocal}
	ts, _ := newTestServer(t, b, nil)

	resp, body := do(t, ts, "POST", "/api/v1/tickets/AGT-01/complete", nil)
	if resp.StatusCode != 409 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var e apitypes.Error
	_ = json.Unmarshal(body, &e)
	if e.MergeRequired == nil || e.MergeRequired.Branch != "agent/agt-01" || e.MergeRequired.Strategy != "local" {
		t.Errorf("error = %+v", e)
	}

	b.completeErr = nil
	resp, body = do(t, ts, "POST", "/api/v1/tickets/AGT-01/complete", nil)
	if resp.StatusCode != 200 || b.tickets["AGT-01"].Status != "done" {
		t.Errorf("status %d: %s", resp.StatusCode, body)
	}

	b.mergeResult = orchestrator.MergeResult{Outcome: orchestrator.MergeOutcomePRCreated, PRURL: "https://x/pr/1"}
	resp, body = do(t, ts, "POST", "/api/v1/tickets/AGT-01/merge", apitypes.Merge{Strategy: "pr"})
	var mr apitypes.MergeResult
	_ = json.Unmarshal(body, &mr)
	if resp.StatusCode != 200 || mr.Outcome != "pr_created" || mr.PRURL != "https://x/pr/1" {
		t.Errorf("merge status %d: %s", resp.StatusCode, body)
	}
}

func TestSessionsInputStopScreenResolve(t *testing.T) {
	b := newFakeBoard()
	b.sessions = []*orchestrator.AgentSession{{SessionID: "SES-01", TicketID: "AGT-01", Agent: "claude", Status: "running", WindowID: "@3"}}
	a := &fakeActivity{
		states:  map[string]activity.State{"SES-01": activity.StateBlocked},
		waiting: []activity.Session{{ID: "SES-01", State: activity.StateBlocked, ToolName: "Bash", Detail: "rm -rf build", Decidable: true}},
	}
	ts, _ := newTestServer(t, b, a)

	resp, body := do(t, ts, "GET", "/api/v1/sessions", nil)
	var list []apitypes.Session
	_ = json.Unmarshal(body, &list)
	if resp.StatusCode != 200 || len(list) != 1 || list[0].Activity != "blocked" || list[0].Detail != "rm -rf build" || !list[0].Decidable || list[0].WindowID != "@3" {
		t.Errorf("sessions = %s", body)
	}

	resp, _ = do(t, ts, "POST", "/api/v1/sessions/SES-01/input", apitypes.Input{Text: "yes"})
	if resp.StatusCode != 204 || b.lastInput != [2]string{"SES-01", "yes"} {
		t.Errorf("input status %d, got %v", resp.StatusCode, b.lastInput)
	}
	b.inputErr = orchestrator.ErrInputSuppressed
	resp, _ = do(t, ts, "POST", "/api/v1/sessions/SES-01/input", apitypes.Input{Text: "yes"})
	if resp.StatusCode != 409 {
		t.Errorf("suppressed input status %d", resp.StatusCode)
	}

	resp, body = do(t, ts, "GET", "/api/v1/sessions/SES-01/screen?lines=20", nil)
	var screen apitypes.Screen
	_ = json.Unmarshal(body, &screen)
	if resp.StatusCode != 200 || screen.Content != "screen of SES-01 (20)" {
		t.Errorf("screen = %s", body)
	}

	resp, _ = do(t, ts, "POST", "/api/v1/sessions/SES-01/resolve", apitypes.Resolve{Decision: "allow"})
	if resp.StatusCode != 204 || a.resolved != [2]string{"SES-01", "allow"} {
		t.Errorf("resolve status %d, got %v", resp.StatusCode, a.resolved)
	}
	resp, _ = do(t, ts, "POST", "/api/v1/sessions/SES-01/resolve", apitypes.Resolve{Decision: "maybe"})
	if resp.StatusCode != 400 {
		t.Errorf("bad decision status %d", resp.StatusCode)
	}
	resp, _ = do(t, ts, "POST", "/api/v1/sessions/SES-99/resolve", apitypes.Resolve{Decision: "deny"})
	if resp.StatusCode != 404 {
		t.Errorf("unheld resolve status %d", resp.StatusCode)
	}

	resp, _ = do(t, ts, "POST", "/api/v1/sessions/SES-01/stop", nil)
	if resp.StatusCode != 204 || b.lastStopped != "SES-01" {
		t.Errorf("stop status %d", resp.StatusCode)
	}
}

func TestResolveWithoutReceiverIsConflict(t *testing.T) {
	ts, _ := newTestServer(t, newFakeBoard(), nil)
	resp, _ := do(t, ts, "POST", "/api/v1/sessions/SES-01/resolve", apitypes.Resolve{Decision: "allow"})
	if resp.StatusCode != 409 {
		t.Errorf("status %d", resp.StatusCode)
	}
}

func TestStartRefusesNonLoopback(t *testing.T) {
	s := &api.Server{Addr: "0.0.0.0:0", Board: newFakeBoard()}
	if _, err := s.Start(); !errors.Is(err, api.ErrNotLoopback) {
		t.Fatalf("err = %v, want ErrNotLoopback", err)
	}
}

func TestStartTreatsBarePortAsLoopback(t *testing.T) {
	s := &api.Server{Addr: ":0", Board: newFakeBoard()}
	base, err := s.Start()
	if err != nil {
		t.Fatalf("a bare :port should bind loopback, got %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Errorf("base = %q", base)
	}
}

func TestStartBindsLoopbackAndStreamsCompletions(t *testing.T) {
	b := newFakeBoard()
	a := &fakeActivity{alerts: make(chan activity.Alert, 4)}
	s := &api.Server{Addr: "127.0.0.1:0", Board: b, Activity: a}
	base, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Fatalf("base = %q", base)
	}

	// Completions and alerts arrive on separate forwarders, so their order
	// is only fixed when they are sent one at a time.
	b.completions <- orchestrator.RunCompletion{TicketID: "AGT-01", SessionID: "SES-01", Outcome: "success"}
	events := readEvents(t, base+"/api/v1/events", 1)
	if events[0].Type != "run.completed" || events[0].Seq != 1 {
		t.Errorf("first event = %+v", events[0])
	}

	a.alerts <- activity.Alert{SessionID: "SES-01", State: activity.StateBlocked, Title: "Bash"}
	replay := readEvents(t, base+"/api/v1/events?after=1", 1)
	if replay[0].Type != "activity.alert" || replay[0].Seq != 2 {
		t.Errorf("replay after=1 gave %+v", replay[0])
	}
}

func TestMutationsAppearOnTheStream(t *testing.T) {
	b := newFakeBoard()
	s := &api.Server{Addr: "127.0.0.1:0", Board: b}
	base, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	resp, err := http.Post(base+"/api/v1/tickets", "application/json", strings.NewReader(`{"title":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	events := readEvents(t, base+"/api/v1/events", 1)
	if events[0].Type != "ticket.created" {
		t.Errorf("event = %+v", events[0])
	}
}

// readEvents opens the stream and returns the first n events, failing after
// a short wait so a broken stream never hangs the suite.
func readEvents(t *testing.T, url string, n int) []apitypes.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}
	var out []apitypes.Event
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() && len(out) < n {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var e apitypes.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e); err != nil {
			t.Fatalf("bad event line %q: %v", line, err)
		}
		out = append(out, e)
	}
	if len(out) < n {
		t.Fatalf("got %d events, want %d", len(out), n)
	}
	return out
}
