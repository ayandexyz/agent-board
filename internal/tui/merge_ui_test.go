package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ayan-de/agent-board/internal/orchestrator"
	"github.com/ayan-de/agent-board/internal/store"

	tea "github.com/charmbracelet/bubbletea"
)

func sized(t *testing.T, app *App) *App {
	t.Helper()
	m, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return m.(*App)
}

func keyRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestStatusChangeRoutesThroughService(t *testing.T) {
	app, fo := newTestApp(t)
	ticket, err := app.store.CreateTicket(context.Background(), store.Ticket{Title: "T", Status: "backlog"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	m, _ := app.Update(statusChangedMsg{ticketID: ticket.ID, newStatus: "in_progress"})
	app = m.(*App)

	if fo.lastMoveTicketID != ticket.ID || fo.lastMoveStatus != "in_progress" {
		t.Errorf("MoveTicketStatus not routed through service: got %q/%q", fo.lastMoveTicketID, fo.lastMoveStatus)
	}
}

func TestStatusChangeServiceErrorKeepsStore(t *testing.T) {
	app, fo := newTestApp(t)
	ticket, _ := app.store.CreateTicket(context.Background(), store.Ticket{Title: "T", Status: "review"})
	fo.moveStatusErr = errors.New("boom")

	m, _ := app.Update(statusChangedMsg{ticketID: ticket.ID, newStatus: "done"})
	app = m.(*App)

	got, _ := app.store.GetTicket(context.Background(), ticket.ID)
	if got.Status != "review" {
		t.Errorf("store status = %q, want review (untouched on service error)", got.Status)
	}
}

func TestMergeRequestOpensMergeModal(t *testing.T) {
	app, fo := newTestApp(t)
	app = sized(t, app)
	ticket, _ := app.store.CreateTicket(context.Background(), store.Ticket{Title: "T", Status: "review"})
	fo.completeErr = &orchestrator.MergeRequiredError{
		TicketID: ticket.ID,
		Strategy: orchestrator.MergeStrategyLocal,
		Branch:   "agent/agt-01",
	}

	m, _ := app.Update(mergeTicketRequestMsg{ticketID: ticket.ID})
	app = m.(*App)

	if !app.mergeModal.Active() {
		t.Fatal("merge modal should be active")
	}
	view := app.View()
	if !strings.Contains(view, "Merge "+ticket.ID) {
		t.Errorf("view should contain merge title, got:\n%s", view)
	}
	if !strings.Contains(view, "local") {
		t.Error("view should show the default strategy")
	}
	if !strings.Contains(view, "agent/agt-01") {
		t.Error("view should show the branch")
	}
}

func TestMergeModalStrategyToggle(t *testing.T) {
	app, fo := newTestApp(t)
	app = sized(t, app)
	ticket, _ := app.store.CreateTicket(context.Background(), store.Ticket{Title: "T", Status: "review"})
	fo.completeErr = &orchestrator.MergeRequiredError{TicketID: ticket.ID, Strategy: orchestrator.MergeStrategyLocal}

	m, _ := app.Update(mergeTicketRequestMsg{ticketID: ticket.ID})
	app = m.(*App)

	m, _ = app.Update(keyRune('s'))
	app = m.(*App)

	if !strings.Contains(app.View(), "pr") {
		t.Error("strategy should toggle to pr after pressing s")
	}

	m, _ = app.Update(keyRune('s'))
	app = m.(*App)
	if !strings.Contains(app.View(), "local") {
		t.Error("strategy should toggle back to local")
	}
}

func TestMergeModalConfirmCallsMergeTicket(t *testing.T) {
	app, fo := newTestApp(t)
	app = sized(t, app)
	ticket, _ := app.store.CreateTicket(context.Background(), store.Ticket{Title: "T", Status: "review"})
	fo.completeErr = &orchestrator.MergeRequiredError{TicketID: ticket.ID, Strategy: orchestrator.MergeStrategyLocal}
	fo.mergeResult = orchestrator.MergeResult{Outcome: orchestrator.MergeOutcomeMerged, Branch: "agent/x"}

	m, _ := app.Update(mergeTicketRequestMsg{ticketID: ticket.ID})
	app = m.(*App)

	// toggle to pr, move cursor to confirm, press enter
	m, _ = app.Update(keyRune('s'))
	app = m.(*App)
	m, _ = app.Update(keyRune('h'))
	app = m.(*App)
	m, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = m.(*App)

	if cmd == nil {
		t.Fatal("enter on confirm should produce a command")
	}
	msg := cmd()
	confirm, ok := msg.(mergeConfirmMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want mergeConfirmMsg", msg)
	}
	if confirm.strategy != orchestrator.MergeStrategyPR {
		t.Errorf("confirm strategy = %q, want pr (toggled)", confirm.strategy)
	}

	m, cmd = app.Update(confirm)
	app = m.(*App)
	if cmd == nil {
		t.Fatal("mergeConfirmMsg should produce a merge command")
	}
	resultMsg := cmd()
	if !fo.mergeCalled {
		t.Fatal("MergeTicket should be called")
	}
	if fo.lastMergeStrategy != orchestrator.MergeStrategyPR {
		t.Errorf("MergeTicket strategy = %q, want pr", fo.lastMergeStrategy)
	}

	if _, ok := resultMsg.(mergeResultMsg); !ok {
		t.Fatalf("merge command produced %T, want mergeResultMsg", resultMsg)
	}
	if app.mergeModal.Active() {
		t.Error("merge modal should close after confirm")
	}
}

func TestMergeModalCancel(t *testing.T) {
	app, fo := newTestApp(t)
	app = sized(t, app)
	ticket, _ := app.store.CreateTicket(context.Background(), store.Ticket{Title: "T", Status: "review"})
	fo.completeErr = &orchestrator.MergeRequiredError{TicketID: ticket.ID, Strategy: orchestrator.MergeStrategyLocal}

	m, _ := app.Update(mergeTicketRequestMsg{ticketID: ticket.ID})
	app = m.(*App)

	m, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = m.(*App)

	if app.mergeModal.Active() {
		t.Error("esc should close the merge modal")
	}
	if fo.mergeCalled {
		t.Error("MergeTicket should not be called on cancel")
	}
}

func TestMergeResultConflictShowsError(t *testing.T) {
	app, _ := newTestApp(t)
	app = sized(t, app)

	m, _ := app.Update(mergeResultMsg{
		ticketID: "AGT-01",
		result:   orchestrator.MergeResult{Outcome: orchestrator.MergeOutcomeConflict, Detail: "CONFLICT in foo.go"},
	})
	app = m.(*App)

	view := app.View()
	if !strings.Contains(view, "conflict") && !strings.Contains(view, "Conflict") {
		t.Errorf("view should surface the conflict, got:\n%s", view)
	}
}

func TestMergeRequiredWithOpenPRShowsNotification(t *testing.T) {
	app, fo := newTestApp(t)
	app = sized(t, app)
	ticket, _ := app.store.CreateTicket(context.Background(), store.Ticket{Title: "T", Status: "review"})
	fo.completeErr = &orchestrator.MergeRequiredError{
		TicketID: ticket.ID,
		Strategy: orchestrator.MergeStrategyPR,
		PRURL:    "https://github.com/u/r/pull/7",
	}

	m, _ := app.Update(mergeTicketRequestMsg{ticketID: ticket.ID})
	app = m.(*App)

	if app.mergeModal.Active() {
		t.Error("modal should not open when a PR already exists")
	}
	if !strings.Contains(app.View(), "pull/7") {
		t.Error("view should surface the PR URL")
	}
}

func openTicketView(t *testing.T, app *App, ticket store.Ticket) *App {
	t.Helper()
	app.view = viewTicket
	app.activeTicket = &ticket
	app.ticketView = app.ticketView.SetTicket(&ticket)
	return app
}

func TestMergeKeyOnReviewTicketOpensModal(t *testing.T) {
	app, fo := newTestApp(t)
	app = sized(t, app)
	ticket, _ := app.store.CreateTicket(context.Background(), store.Ticket{Title: "T", Status: "review"})
	app = openTicketView(t, app, ticket)
	fo.completeErr = &orchestrator.MergeRequiredError{
		TicketID: ticket.ID,
		Strategy: orchestrator.MergeStrategyLocal,
		Branch:   "agent/x",
	}

	m, cmd := app.Update(keyRune('m'))
	app = m.(*App)
	if cmd == nil {
		t.Fatal("m on a review ticket should produce a command")
	}
	m, _ = app.Update(cmd())
	app = m.(*App)

	if !app.mergeModal.Active() {
		t.Fatal("merge modal should open after pressing m on a review ticket")
	}
}

func TestMergeKeyOnNonReviewTicketShowsHint(t *testing.T) {
	app, fo := newTestApp(t)
	app = sized(t, app)
	ticket, _ := app.store.CreateTicket(context.Background(), store.Ticket{Title: "T", Status: "backlog"})
	app = openTicketView(t, app, ticket)

	m, cmd := app.Update(keyRune('m'))
	app = m.(*App)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			m, _ = app.Update(msg)
			app = m.(*App)
		}
	}

	if app.mergeModal.Active() {
		t.Error("merge modal must not open for a non-review ticket")
	}
	if fo.mergeCalled {
		t.Error("MergeTicket must not be called for a non-review ticket")
	}
	if !strings.Contains(app.View(), "Review") {
		t.Error("a hint mentioning Review should be shown")
	}
}

func TestMergeKeyNoUnmergedWorkMovesToDone(t *testing.T) {
	app, fo := newTestApp(t)
	app = sized(t, app)
	ticket, _ := app.store.CreateTicket(context.Background(), store.Ticket{Title: "T", Status: "review"})
	app = openTicketView(t, app, ticket)

	m, cmd := app.Update(keyRune('m'))
	app = m.(*App)
	if cmd == nil {
		t.Fatal("m on a review ticket should produce a command")
	}
	m, _ = app.Update(cmd())
	app = m.(*App)

	if fo.lastCompletedTicket != ticket.ID {
		t.Errorf("ticket without unmerged work should be completed, got %q", fo.lastCompletedTicket)
	}
}

func TestReviewToDoneStatusChangeNeverOpensMergeModal(t *testing.T) {
	app, fo := newTestApp(t)
	app = sized(t, app)
	ticket, _ := app.store.CreateTicket(context.Background(), store.Ticket{Title: "T", Status: "review", WorktreePath: "/wt/x", Branch: "agent/x"})
	fo.completeErr = &orchestrator.MergeRequiredError{TicketID: ticket.ID, Strategy: orchestrator.MergeStrategyLocal, Branch: "agent/x"}

	m, _ := app.Update(statusChangedMsg{ticketID: ticket.ID, newStatus: "done"})
	app = m.(*App)

	if app.mergeModal.Active() {
		t.Fatal("a plain status change must not open the merge modal")
	}
	if fo.lastCompletedTicket != "" {
		t.Fatal("a plain status change must not go through CompleteTicket")
	}
	got, _ := app.store.GetTicket(context.Background(), ticket.ID)
	if got.Status != "done" {
		t.Errorf("status = %q, want done", got.Status)
	}
}

func TestDeleteTicketRoutesThroughService(t *testing.T) {
	app, fo := newTestApp(t)
	ticket, _ := app.store.CreateTicket(context.Background(), store.Ticket{Title: "T", Status: "backlog"})

	m, _ := app.Update(deleteTicketConfirmMsg{ticketID: ticket.ID})
	app = m.(*App)

	if fo.lastDeletedTicket != ticket.ID {
		t.Errorf("DeleteTicket not routed through service, got %q", fo.lastDeletedTicket)
	}
	if _, err := app.store.GetTicket(context.Background(), ticket.ID); err == nil {
		t.Error("ticket should be deleted")
	}
}

func TestDeleteTicketDirtyRefusalKeepsTicket(t *testing.T) {
	app, fo := newTestApp(t)
	app = sized(t, app)
	ticket, _ := app.store.CreateTicket(context.Background(), store.Ticket{Title: "T", Status: "backlog"})
	fo.deleteErr = errors.New("worktree has uncommitted changes")

	m, _ := app.Update(deleteTicketConfirmMsg{ticketID: ticket.ID})
	app = m.(*App)

	if _, err := app.store.GetTicket(context.Background(), ticket.ID); err != nil {
		t.Error("ticket must survive a refused delete")
	}
}
