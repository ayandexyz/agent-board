package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/ayandexyz/agent-board/internal/store"

	tea "github.com/charmbracelet/bubbletea"
)

var editKey = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")}

func branchFieldCursor(m TicketViewModel, t *testing.T) int {
	t.Helper()
	for i, f := range m.fields {
		if f.label == "Branch" {
			return i
		}
	}
	t.Fatal("Branch field not found")
	return -1
}

func newBranchTicketView(t *testing.T) (TicketViewModel, *store.Store, *store.Ticket) {
	t.Helper()
	m, s := newTestTicketView(t)
	ticket, err := s.CreateTicket(context.Background(), store.Ticket{Title: "T", Status: "backlog"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	m = m.SetGitContext("feat/live", "agent/")
	m = m.SetTicket(&ticket)
	m.cursor = branchFieldCursor(m, t)
	m.width, m.height = 100, 40
	return m, s, &ticket
}

func TestBranchFieldEnterOpensModeToggle(t *testing.T) {
	m, _, _ := newBranchTicketView(t)

	m, _ = m.Update(editKey)
	if m.mode != ticketBranchModeSelectMode {
		t.Fatalf("mode = %v, want ticketBranchModeSelectMode", m.mode)
	}

	view := m.View()
	if !strings.Contains(view, "Current branch") || !strings.Contains(view, "Worktree branch") {
		t.Errorf("toggle options missing from view:\n%s", view)
	}
}

func TestSelectCurrentBranchModeSkipsWorktree(t *testing.T) {
	m, s, ticket := newBranchTicketView(t)

	m, _ = m.Update(editKey)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.mode != ticketViewMode {
		t.Errorf("mode = %v, want ticketViewMode", m.mode)
	}
	got, err := s.GetTicket(context.Background(), ticket.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.BranchMode != store.BranchModeCurrent {
		t.Errorf("BranchMode = %q, want %q", got.BranchMode, store.BranchModeCurrent)
	}
	if got.Branch != "" {
		t.Errorf("Branch = %q, want empty for current-branch mode", got.Branch)
	}
	if !strings.Contains(m.View(), "feat/live") {
		t.Errorf("view should name the current branch:\n%s", m.View())
	}
}

func TestSelectWorktreeBranchModePrefillsBranchName(t *testing.T) {
	m, s, ticket := newBranchTicketView(t)

	m, _ = m.Update(editKey)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.mode != ticketEditMode {
		t.Fatalf("mode = %v, want ticketEditMode", m.mode)
	}
	want := "agent/" + strings.ToLower(ticket.ID)
	if m.editBuffer != want {
		t.Fatalf("editBuffer = %q, want %q", m.editBuffer, want)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-x")})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	got, err := s.GetTicket(context.Background(), ticket.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.Branch != want+"-x" {
		t.Errorf("Branch = %q, want %q", got.Branch, want+"-x")
	}
	if got.BranchMode != store.BranchModeWorktree {
		t.Errorf("BranchMode = %q, want %q", got.BranchMode, store.BranchModeWorktree)
	}
}

func TestBranchModeToggleOpensOnTicketsCurrentMode(t *testing.T) {
	m, s, ticket := newBranchTicketView(t)
	ticket.BranchMode = store.BranchModeCurrent
	if _, err := s.UpdateTicket(context.Background(), *ticket); err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}

	m, _ = m.Update(editKey)
	if m.branchModeCursor != 0 {
		t.Errorf("branchModeCursor = %d, want 0 (Current branch)", m.branchModeCursor)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	ticket.BranchMode = store.BranchModeWorktree
	m = m.SetTicket(ticket)
	m.cursor = branchFieldCursor(m, t)
	m, _ = m.Update(editKey)
	if m.branchModeCursor != 1 {
		t.Errorf("branchModeCursor = %d, want 1 (Worktree branch)", m.branchModeCursor)
	}
}
