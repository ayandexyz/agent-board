package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ayandexyz/agent-board/internal/orchestrator"
	"github.com/ayandexyz/agent-board/internal/store"

	tea "github.com/charmbracelet/bubbletea"
)

// movingBoard records SetTicketRepo and answers with a ticket that now lives
// in another repository.
type movingBoard struct {
	*fakeOrchestrator
	gotID, gotRepo string
	moveErr        error
}

func (b *movingBoard) SetTicketRepo(_ context.Context, id, repo string) (store.Ticket, error) {
	b.gotID, b.gotRepo = id, repo
	if b.moveErr != nil {
		return store.Ticket{}, b.moveErr
	}
	return store.Ticket{ID: "FRE-01", Title: "T", Status: "backlog", Repo: repo}, nil
}

func repoFieldCursor(m TicketViewModel, t *testing.T) int {
	t.Helper()
	for i, f := range m.fields {
		if f.label == "Repo" {
			return i
		}
	}
	t.Fatal("Repo field not found")
	return -1
}

func newRepoTicketView(t *testing.T, moveErr error) (TicketViewModel, *movingBoard) {
	t.Helper()
	m, s := newTestTicketView(t)
	board := &movingBoard{fakeOrchestrator: newFakeOrchestrator(s), moveErr: moveErr}
	m.board = board
	ticket, err := s.CreateTicket(context.Background(), store.Ticket{Title: "T", Status: "backlog"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	m = m.SetTicket(&ticket)
	m.cursor = repoFieldCursor(m, t)
	m.width, m.height = 100, 40
	return m, board
}

func typeText(m TicketViewModel, s string) TicketViewModel {
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return m
}

func TestRepoFieldMovesTheTicketToTheTypedRepository(t *testing.T) {
	m, board := newRepoTicketView(t, nil)

	m, _ = m.Update(editKey)
	if m.mode != ticketEditMode {
		t.Fatalf("mode = %v, want ticketEditMode", m.mode)
	}
	m = typeText(m, "/src/freecode")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if board.gotID != "AGT-01" || board.gotRepo != "/src/freecode" {
		t.Errorf("SetTicketRepo(%q, %q), want (AGT-01, /src/freecode)", board.gotID, board.gotRepo)
	}
	if m.ticket.ID != "FRE-01" || m.ticket.Repo != "/src/freecode" {
		t.Errorf("view shows %s in %q, want the moved FRE-01", m.ticket.ID, m.ticket.Repo)
	}
	if cmd == nil {
		t.Fatal("no message after the move")
	}
	moved, ok := cmd().(ticketMovedMsg)
	if !ok || moved.from != "AGT-01" || moved.to != "FRE-01" {
		t.Errorf("cmd() = %#v, want ticketMovedMsg AGT-01 -> FRE-01", cmd())
	}
}

func TestRepoFieldExpandsHomeDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	m, board := newRepoTicketView(t, nil)

	m, _ = m.Update(editKey)
	m = typeText(m, "~/Projects/freecode")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if want := filepath.Join(home, "Projects", "freecode"); board.gotRepo != want {
		t.Errorf("repo = %q, want %q", board.gotRepo, want)
	}
}

func TestRepoFieldRefusalKeepsTheTicketAndSaysWhy(t *testing.T) {
	m, _ := newRepoTicketView(t, orchestrator.ErrRepoLocked)

	m, _ = m.Update(editKey)
	m = typeText(m, "/src/freecode")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.ticket.ID != "AGT-01" {
		t.Errorf("ticket = %s, want AGT-01 unchanged", m.ticket.ID)
	}
	if cmd == nil {
		t.Fatal("no notification for the refused move")
	}
	n, ok := cmd().(notificationMsg)
	if !ok || n.variant != NotificationError || !strings.Contains(n.message, "work in its repository") {
		t.Errorf("cmd() = %#v, want an error notification naming the reason", cmd())
	}
}

func TestRepoFieldShowsThisBoardWhenTicketIsLocal(t *testing.T) {
	m, _ := newRepoTicketView(t, nil)
	if !strings.Contains(m.View(), "this board") {
		t.Errorf("Repo row does not say the ticket is on this board:\n%s", m.View())
	}
}

func TestCardNamesTheRepositoryOfAForeignTicket(t *testing.T) {
	tk := testTicket()
	tk.Repo = "/src/freecode"
	out := NewTicketCardModel(tk, false, false, 40, 0, testCardTheme()).Render()
	if !strings.Contains(out, "freecode") {
		t.Errorf("card does not name its repository:\n%s", out)
	}
}

func TestCardOfALocalTicketNamesNoRepository(t *testing.T) {
	tk := testTicket()
	out := NewTicketCardModel(tk, false, false, 40, 0, testCardTheme()).Render()
	if strings.Contains(out, "⌂") {
		t.Errorf("local card carries a repository tag:\n%s", out)
	}
}

func TestBoardSyncPicksUpTicketsWrittenByAnotherBoard(t *testing.T) {
	app, fo := newTestApp(t)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	app = model.(*App)
	if _, err := fo.store.CreateTicket(context.Background(), store.Ticket{Title: "made elsewhere", Status: "backlog"}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	model, cmd := app.Update(boardSyncMsg{})
	app = model.(*App)

	if !strings.Contains(app.View(), "made elsewhere") {
		t.Errorf("board did not reload after a sync tick:\n%s", app.View())
	}
	if cmd == nil {
		t.Error("sync tick did not schedule the next one")
	}
}
