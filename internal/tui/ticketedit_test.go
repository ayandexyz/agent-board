package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/ayan-de/agent-board/internal/store"

	tea "github.com/charmbracelet/bubbletea"
)

func newEditingTicketView(t *testing.T, prompt string) TicketViewModel {
	t.Helper()
	m, s := newTestTicketView(t)
	m.width, m.height = 90, 30
	ticket, err := s.CreateTicket(context.Background(), store.Ticket{Title: "T", Prompt: prompt, Status: "backlog"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	m = m.SetTicket(&ticket)
	for i, f := range m.fields {
		if f.label == "Prompt" {
			m.cursor = i
		}
	}
	m, _ = m.Update(editKey)
	if m.mode != ticketEditMode {
		t.Fatalf("mode = %v, want ticketEditMode", m.mode)
	}
	return m
}

func typeRunes(m TicketViewModel, s string) TicketViewModel {
	for _, r := range s {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestEditCursorStartsAtEndOfBuffer(t *testing.T) {
	m := newEditingTicketView(t, "hello")
	if m.editCursor != len([]rune("hello")) {
		t.Errorf("editCursor = %d, want %d", m.editCursor, len("hello"))
	}
}

func TestEditInsertsAtCursor(t *testing.T) {
	m := newEditingTicketView(t, "helo")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = typeRunes(m, "l")

	if m.editBuffer != "hello" {
		t.Errorf("editBuffer = %q, want %q", m.editBuffer, "hello")
	}
	if m.editCursor != 3 {
		t.Errorf("editCursor = %d, want 3", m.editCursor)
	}
}

func TestEditBackspaceAndDeleteAtCursor(t *testing.T) {
	m := newEditingTicketView(t, "hello")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.editBuffer != "helo" {
		t.Errorf("after backspace editBuffer = %q, want %q", m.editBuffer, "helo")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDelete})
	if m.editBuffer != "hel" {
		t.Errorf("after delete editBuffer = %q, want %q", m.editBuffer, "hel")
	}
}

func TestEditHomeEndAndWordDelete(t *testing.T) {
	m := newEditingTicketView(t, "feat/live branch")

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	if m.editCursor != 0 {
		t.Errorf("editCursor = %d after home, want 0", m.editCursor)
	}
	m = typeRunes(m, "x")
	if m.editBuffer != "xfeat/live branch" {
		t.Errorf("editBuffer = %q", m.editBuffer)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if m.editCursor != len([]rune(m.editBuffer)) {
		t.Errorf("editCursor = %d after end, want %d", m.editCursor, len([]rune(m.editBuffer)))
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	if m.editBuffer != "xfeat/live " {
		t.Errorf("editBuffer = %q after ctrl+w, want %q", m.editBuffer, "xfeat/live ")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if m.editBuffer != "" {
		t.Errorf("editBuffer = %q after ctrl+u, want empty", m.editBuffer)
	}
}

func TestEditCursorDoesNotRunPastEnds(t *testing.T) {
	m := newEditingTicketView(t, "ab")
	for i := 0; i < 5; i++ {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	}
	if m.editCursor != 0 {
		t.Errorf("editCursor = %d, want 0", m.editCursor)
	}
	for i := 0; i < 5; i++ {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	}
	if m.editCursor != 2 {
		t.Errorf("editCursor = %d, want 2", m.editCursor)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDelete})
	if m.editBuffer != "ab" {
		t.Errorf("delete at end changed buffer to %q", m.editBuffer)
	}
}

func TestBranchEditPrefillPlacesCursorAtEnd(t *testing.T) {
	m, _, ticket := newBranchTicketView(t)
	m, _ = m.Update(editKey)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	want := "agent/" + strings.ToLower(ticket.ID)
	if m.editCursor != len([]rune(want)) {
		t.Fatalf("editCursor = %d, want %d", m.editCursor, len([]rune(want)))
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = typeRunes(m, "wip-")
	if m.editBuffer != "wip-"+want {
		t.Errorf("editBuffer = %q, want %q", m.editBuffer, "wip-"+want)
	}
}

func TestAdHocPromptEditsAtCursor(t *testing.T) {
	m, s := newTestTicketView(t)
	m.width, m.height = 90, 30
	ticket, _ := s.CreateTicket(context.Background(), store.Ticket{Title: "T", Status: "backlog"})
	m = m.SetTicket(&ticket)
	m.mode = ticketAdHocPromptMode
	m.adhocAgent = "claude-code"

	m = typeRunes(m, "run tests")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = typeRunes(m, "please ")

	if m.adhocPrompt != "please run tests" {
		t.Errorf("adhocPrompt = %q, want %q", m.adhocPrompt, "please run tests")
	}
	if m.adhocPromptCursor != len([]rune("please ")) {
		t.Errorf("adhocPromptCursor = %d, want %d", m.adhocPromptCursor, len("please "))
	}
}
