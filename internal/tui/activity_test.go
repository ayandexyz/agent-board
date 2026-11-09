package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/ayandexyz/agent-board/internal/activity"
	"github.com/ayandexyz/agent-board/internal/store"
)

type fakeActivity struct {
	states map[string]activity.State
}

func (f *fakeActivity) StateOf(sessionID string) activity.State { return f.states[sessionID] }

func (f *fakeActivity) Waiting() []activity.Session {
	var out []activity.Session
	for id, st := range f.states {
		if st.NeedsInput() {
			out = append(out, activity.Session{ID: id, State: st, ToolName: "Bash", Detail: "ls", Decidable: st == activity.StateBlocked})
		}
	}
	return out
}

func startSessions(t *testing.T, a *App, tickets ...string) []string {
	t.Helper()
	var ids []string
	for _, tk := range tickets {
		if tk != "" {
			if _, err := a.testStore().CreateTicket(context.Background(), store.Ticket{ID: tk, Title: tk, Status: "in_progress", Priority: "medium"}); err != nil {
				t.Fatal(err)
			}
		}
		s, err := a.testStore().CreateSession(context.Background(), store.Session{TicketID: tk, Agent: "claude-code", Status: "running"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, s.ID)
	}
	return ids
}

func TestRefreshActivityMapsSessionsToTickets(t *testing.T) {
	a, _ := newTestApp(t)
	ids := startSessions(t, a, "AGT-01", "AGT-02", "")
	fa := &fakeActivity{states: map[string]activity.State{
		ids[0]: activity.StateBlocked,
		ids[1]: activity.StateActive,
		ids[2]: activity.StateWaitingInput,
	}}
	a.activityReader = fa

	a.refreshActivity()
	if got := a.activityByTicket["AGT-01"]; got != "blocked" {
		t.Fatalf("AGT-01 = %q, want blocked", got)
	}
	if got := a.activityByTicket["AGT-02"]; got != "active" {
		t.Fatalf("AGT-02 = %q, want active", got)
	}
	if a.waitingCount != 2 {
		t.Fatalf("waitingCount = %d, want 2 (blocked ticket + ad-hoc question)", a.waitingCount)
	}
	if got := a.kanban.activity["AGT-01"]; got != "blocked" {
		t.Fatalf("kanban did not receive activity: %v", a.kanban.activity)
	}
}

func TestRefreshActivityWithoutReader(t *testing.T) {
	a, _ := newTestApp(t)
	startSessions(t, a, "AGT-01")
	a.refreshActivity()
	if a.waitingCount != 0 || len(a.activityByTicket) != 0 {
		t.Fatalf("activity tracked with no reader: %d %v", a.waitingCount, a.activityByTicket)
	}
}

func TestHeaderShowsWaitingCount(t *testing.T) {
	a, _ := newTestApp(t)
	a.width = 120
	a.waitingCount = 2
	if !strings.Contains(a.renderHeader(), "2 waiting") {
		t.Fatalf("header = %q", a.renderHeader())
	}
	a.waitingCount = 0
	if strings.Contains(a.renderHeader(), "waiting") {
		t.Fatal("header shows waiting with none pending")
	}
}

func TestWaitingItemsCarryTicketAndDetail(t *testing.T) {
	a, _ := newTestApp(t)
	ids := startSessions(t, a, "AGT-01")
	a.activityReader = &fakeActivity{states: map[string]activity.State{ids[0]: activity.StateBlocked}}
	a.resolvePermission = func(string, string) {}

	items := a.waitingItems()
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	it := items[0]
	if it.SessionID != ids[0] || it.TicketID != "AGT-01" || it.State != "blocked" || it.ToolName != "Bash" || it.Detail != "ls" || !it.Decidable {
		t.Fatalf("item = %+v", it)
	}
}

func TestAppResolvesFromOverlay(t *testing.T) {
	a, _ := newTestApp(t)
	var resolved string
	a.resolvePermission = func(sessionID, decision string) { resolved = sessionID + "/" + decision }
	a.activityReader = &fakeActivity{states: map[string]activity.State{}}

	_, _ = a.Update(waitingResolveMsg{sessionID: "SES-01", decision: "deny"})
	if resolved != "SES-01/deny" {
		t.Fatalf("resolved = %q", resolved)
	}
	if a.waiting.Active() {
		t.Fatal("overlay stayed open with nothing left to decide")
	}
}

func TestAppJumpsFromOverlay(t *testing.T) {
	a, fo := newTestApp(t)
	_, _ = a.Update(waitingJumpMsg{sessionID: "SES-07"})
	if fo.lastSwitchedPane != "SES-07" {
		t.Fatalf("SwitchToPane called with %q", fo.lastSwitchedPane)
	}
}

func TestWaitingItemsNotDecidableWithoutResolver(t *testing.T) {
	a, _ := newTestApp(t)
	ids := startSessions(t, a, "AGT-01")
	a.activityReader = &fakeActivity{states: map[string]activity.State{ids[0]: activity.StateBlocked}}
	a.resolvePermission = nil

	items := a.waitingItems()
	if len(items) != 1 || items[0].Decidable {
		t.Fatalf("y/n offered with nobody to deliver the decision: %+v", items)
	}
}
