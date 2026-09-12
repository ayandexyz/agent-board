package tui

import (
	"context"
	"testing"
	"time"

	"github.com/ayan-de/agent-board/internal/store"
)

func TestCompletionAttributedToItsOwnTicket(t *testing.T) {
	app, fo := newTestApp(t)
	ctx := context.Background()
	for _, id := range []string{"AGT-01", "AGT-02"} {
		if _, err := app.store.CreateTicket(ctx, store.Ticket{ID: id, Title: id, Status: "in_progress", Agent: "claude-code"}); err != nil {
			t.Fatal(err)
		}
	}

	// Two runs started in order; the second finishes first.
	execCmd(app, app.startDirectRunAndListenCmd("AGT-01", "claude-code", "p"))
	execCmd(app, app.startDirectRunAndListenCmd("AGT-02", "claude-code", "p"))

	listen := app.waitForCompletionCmd()
	if listen == nil {
		t.Fatal("no completion listener")
	}
	fo.completeRun("AGT-02", "SES-02", "completed")

	msgCh := make(chan runCompletedMsg, 1)
	go func() {
		msg, _ := listen().(runCompletedMsg)
		msgCh <- msg
	}()
	select {
	case msg := <-msgCh:
		if msg.ticketID != "AGT-02" || msg.sessionID != "SES-02" {
			t.Fatalf("completion attributed to %+v, want AGT-02/SES-02", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("listener never delivered the completion")
	}
}

func TestStartCmdsDoNotConsumeCompletions(t *testing.T) {
	app, fo := newTestApp(t)
	ctx := context.Background()
	if _, err := app.store.CreateTicket(ctx, store.Ticket{ID: "AGT-01", Title: "x", Status: "in_progress"}); err != nil {
		t.Fatal(err)
	}

	execCmd(app, app.startDirectRunAndListenCmd("AGT-01", "claude-code", "p"))
	execCmd(app, app.startAdHocRunAndListenCmd("claude-code", "p"))

	fo.completeRun("AGT-01", "SES-01", "completed")
	select {
	case c := <-app.completionCh:
		if c.TicketID != "AGT-01" {
			t.Fatalf("got %+v", c)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("a start cmd swallowed the completion that the listener should see")
	}
}

func TestRunCompletedRearmsListener(t *testing.T) {
	app, _ := newTestApp(t)
	_, cmd := app.Update(runCompletedMsg{ticketID: "AGT-01", sessionID: "SES-01"})
	if cmd == nil {
		t.Fatal("runCompletedMsg returned no cmd; the listener must be re-armed")
	}
}

func TestCompletionListenerEndsWhenUnsubscribed(t *testing.T) {
	app, _ := newTestApp(t)
	listen := app.waitForCompletionCmd()
	app.Close()
	done := make(chan struct{})
	go func() {
		_ = listen()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("listener still blocked after the app closed its subscription")
	}
}
