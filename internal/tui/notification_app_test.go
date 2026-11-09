package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ayandexyz/agent-board/internal/activity"
	"github.com/ayandexyz/agent-board/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func TestAppTicketCreateShowsNotification(t *testing.T) {
	app, _ := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd == nil {
		t.Fatal("ticket create should return ticket created message command")
	}

	_, dismissCmd := app.Update(cmd())
	if dismissCmd == nil {
		t.Fatal("ticket created message should return notification dismiss command")
	}
	if !app.notification.Active() {
		t.Fatal("notification should be active after ticket create")
	}
	if len(app.notification.items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(app.notification.items))
	}
	if app.notification.items[0].title != "Ticket created" {
		t.Fatalf("title = %q, want %q", app.notification.items[0].title, "Ticket created")
	}
	if !strings.Contains(app.notification.items[0].message, "New Ticket") {
		t.Fatalf("message = %q, want to mention new ticket", app.notification.items[0].message)
	}
}

func TestAppTicketCreateNotificationDoesNotBlockNavigation(t *testing.T) {
	app, _ := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd == nil {
		t.Fatal("ticket create should return ticket created message command")
	}
	app.Update(cmd())
	if !app.notification.Active() {
		t.Fatal("notification should be active after ticket create")
	}

	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if app.kanban.colIndex != 1 {
		t.Fatalf("colIndex = %d, want 1", app.kanban.colIndex)
	}
}

func TestAppNotificationDismissIgnoresStaleMessage(t *testing.T) {
	app, _ := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, firstCmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if firstCmd == nil {
		t.Fatal("first ticket create should return ticket created message command")
	}
	_, dismissCmd := app.Update(firstCmd())
	if dismissCmd == nil {
		t.Fatal("ticket created message should return dismiss command")
	}
	firstID := app.notification.items[0].id

	_, secondCmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if secondCmd == nil {
		t.Fatal("second ticket create should return ticket created message command")
	}
	app.Update(secondCmd())
	secondID := app.notification.items[1].id

	app.Update(notificationDismissMsg{id: firstID})

	if !app.notification.Active() {
		t.Fatal("stale dismiss should not clear newer notification")
	}
	if len(app.notification.items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(app.notification.items))
	}
	if app.notification.items[0].id != secondID {
		t.Fatalf("notification id = %d, want %d", app.notification.items[0].id, secondID)
	}
}

func TestAppAgentAssignShowsNotification(t *testing.T) {
	app, _ := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	ctx := context.Background()

	ticket, err := app.testStore().CreateTicket(ctx, store.Ticket{Title: "Assign Me", Status: "backlog"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	app.kanban, _ = app.kanban.Reload()
	app.activeTicket = &ticket
	app.ticketView = app.ticketView.SetTicket(&ticket)
	app.view = viewTicket

	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("agent assign should return agent assigned message command")
	}
	_, dismissCmd := app.Update(cmd())
	if dismissCmd == nil {
		t.Fatal("agent assigned message should return notification dismiss command")
	}
	if !app.notification.Active() {
		t.Fatal("notification should be active after agent assign")
	}
	if len(app.notification.items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(app.notification.items))
	}
	if app.notification.items[0].title != "Agent assignment updated" {
		t.Fatalf("title = %q, want %q", app.notification.items[0].title, "Agent assignment updated")
	}
	if !strings.Contains(app.notification.items[0].message, ticket.ID) {
		t.Fatalf("message = %q, want to mention ticket id", app.notification.items[0].message)
	}
}

func TestAppNotificationsStackInsteadOfReplacing(t *testing.T) {
	app, _ := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, firstCmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if firstCmd == nil {
		t.Fatal("first ticket create should return ticket created message command")
	}
	app.Update(firstCmd())

	_, secondCmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if secondCmd == nil {
		t.Fatal("second ticket create should return ticket created message command")
	}
	app.Update(secondCmd())

	if len(app.notification.items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(app.notification.items))
	}
	if app.notification.items[0].id == app.notification.items[1].id {
		t.Fatal("stacked notifications should have distinct ids")
	}
}

func TestAppActivityAlertShowsNotification(t *testing.T) {
	app, _ := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, cmd := app.Update(activity.Alert{
		SessionID: "SES-01",
		State:     activity.StateBlocked,
		Title:     "agent-1a2b3c4d needs approval",
		Body:      "Bash rm -rf build",
	})
	if cmd == nil {
		t.Fatal("activity alert should return a command")
	}
	if len(app.notification.items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(app.notification.items))
	}
	item := app.notification.items[0]
	if item.title != "agent-1a2b3c4d needs approval" {
		t.Fatalf("title = %q", item.title)
	}
	if item.variant != NotificationError {
		t.Errorf("blocked should render as the error variant, got %v", item.variant)
	}
}

func TestAppActivityAlertRearmsListener(t *testing.T) {
	app, _ := newTestApp(t)
	events := make(chan activity.Alert, 1)
	app.activityEvents = events
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, cmd := app.Update(activity.Alert{SessionID: "SES-01", State: activity.StateWaitingInput, Title: "q"})
	if cmd == nil {
		t.Fatal("expected a batched command")
	}
	// The re-armed listener must pick up the next alert, or only the first
	// "needs you" of a session ever reaches the board.
	events <- activity.Alert{SessionID: "SES-02", State: activity.StateWaitingInput, Title: "second"}
	found := false
	for _, m := range flatten(cmd()) {
		if a, ok := m.(activity.Alert); ok && a.SessionID == "SES-02" {
			found = true
		}
	}
	if !found {
		t.Fatal("listener was not re-armed; the second alert never arrived")
	}
}

// flatten runs a tea.BatchMsg's commands and collects whatever they produce
// within a short window. Concurrent because a batch mixes fast commands with
// ones that sleep for the notification's whole lifetime.
func flatten(msg tea.Msg) []tea.Msg {
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	out := make(chan tea.Msg, len(batch))
	for _, c := range batch {
		if c != nil {
			go func(c tea.Cmd) { out <- c() }(c)
		}
	}
	var msgs []tea.Msg
	deadline := time.After(2 * time.Second)
	for range batch {
		select {
		case m := <-out:
			msgs = append(msgs, m)
		case <-deadline:
			return msgs
		}
	}
	return msgs
}

func TestDecidablePermissionAlertOpensModal(t *testing.T) {
	app, _ := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	got := make(chan [2]string, 2)
	app.resolvePermission = func(sessionID, decision string) { got <- [2]string{sessionID, decision} }

	app.Update(activity.Alert{
		SessionID: "SES-1",
		State:     activity.StateBlocked,
		Title:     "agent-1 needs approval",
		Body:      "Bash: rm -rf build",
		Decidable: true,
	})
	if !app.modal.Active() {
		t.Fatal("decidable alert should open the approval modal")
	}

	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	select {
	case answer := <-got:
		if answer != [2]string{"SES-1", "allow"} {
			t.Fatalf("resolved with %v, want SES-1 allow", answer)
		}
	default:
		t.Fatal("confirming the modal did not resolve the request")
	}
}

func TestUndecidableAlertStaysAToast(t *testing.T) {
	app, _ := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app.resolvePermission = func(string, string) { t.Fatal("must not resolve an undecidable alert") }

	app.Update(activity.Alert{SessionID: "SES-2", State: activity.StateWaitingInput, Title: "x", Body: "y"})
	if app.modal.Active() {
		t.Fatal("alert with no held request should not open a modal")
	}
	if !app.notification.Active() {
		t.Fatal("undecidable alert should still raise a toast")
	}
}
