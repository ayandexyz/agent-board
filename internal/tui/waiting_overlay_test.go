package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestWaitingOverlayKeys(t *testing.T) {
	var o WaitingOverlay
	o.SetSize(80, 24)
	o.Open([]WaitingItem{
		{SessionID: "SES-01", TicketID: "AGT-01", State: "blocked", ToolName: "Bash", Detail: "rm -rf build", Decidable: true},
		{SessionID: "SES-02", TicketID: "AGT-02", State: "waiting_input", ToolName: "AskUserQuestion", Detail: "Which option?"},
	})
	if !o.Active() {
		t.Fatal("not active after Open")
	}
	view := o.ViewBox()
	for _, want := range []string{"AGT-01", "rm -rf build", "AGT-02", "Which option?", "2 waiting"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}

	o, cmd := o.Update(key("y"))
	if cmd == nil {
		t.Fatal("y on a decidable item produced no cmd")
	}
	if msg, ok := cmd().(waitingResolveMsg); !ok || msg.sessionID != "SES-01" || msg.decision != "allow" {
		t.Fatalf("y produced %#v", cmd())
	}

	o, _ = o.Update(key("j"))
	o, cmd = o.Update(key("y"))
	if cmd != nil {
		t.Fatal("y on a session nobody can answer over the wire should do nothing")
	}
	o, cmd = o.Update(key("n"))
	if cmd != nil {
		t.Fatal("n on a session nobody can answer over the wire should do nothing")
	}
	o, cmd = o.Update(key("enter"))
	if msg, ok := cmd().(waitingJumpMsg); !ok || msg.sessionID != "SES-02" {
		t.Fatalf("enter produced %#v", cmd())
	}
	if o.Active() {
		t.Fatal("jump did not close the overlay")
	}

	o.Open([]WaitingItem{{SessionID: "SES-01", TicketID: "AGT-01", State: "blocked", Decidable: true}})
	o, cmd = o.Update(key("n"))
	if msg, ok := cmd().(waitingResolveMsg); !ok || msg.decision != "deny" {
		t.Fatalf("n produced %#v", cmd())
	}
	o, cmd = o.Update(key("esc"))
	if o.Active() {
		t.Fatal("esc did not close")
	}
	if _, ok := cmd().(waitingClosedMsg); !ok {
		t.Fatal("esc did not emit waitingClosedMsg")
	}
}

func TestWaitingOverlayCursorStaysInRange(t *testing.T) {
	var o WaitingOverlay
	o.SetSize(80, 24)
	o.Open([]WaitingItem{{SessionID: "SES-01", TicketID: "AGT-01", State: "blocked"}})
	o, _ = o.Update(key("k"))
	o, _ = o.Update(key("j"))
	o, _ = o.Update(key("j"))
	o, cmd := o.Update(key("enter"))
	if msg, ok := cmd().(waitingJumpMsg); !ok || msg.sessionID != "SES-01" {
		t.Fatalf("cursor left the list: %#v", cmd())
	}
}

func TestWaitingOverlayEmpty(t *testing.T) {
	var o WaitingOverlay
	o.SetSize(80, 24)
	o.Open(nil)
	if !strings.Contains(o.ViewBox(), "No agent is waiting") {
		t.Fatalf("empty view:\n%s", o.ViewBox())
	}
	o, cmd := o.Update(key("enter"))
	if cmd != nil {
		t.Fatal("enter on an empty list produced a cmd")
	}
	o, cmd = o.Update(key("y"))
	if cmd != nil {
		t.Fatal("y on an empty list produced a cmd")
	}
}
