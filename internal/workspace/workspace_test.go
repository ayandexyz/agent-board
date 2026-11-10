package workspace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ayandexyz/agent-board/internal/store"
)

var columns = []string{"backlog", "in_progress", "review", "done"}

func openStore(t *testing.T, prefix string, statuses []string) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "board.db"), statuses, prefix)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// boards is a fake Opener: each repository root maps to a board, and any
// path inside a root resolves to it.
type boards struct {
	byRoot map[string]*store.Store
	opened map[string]int
}

func newBoards() *boards {
	return &boards{byRoot: map[string]*store.Store{}, opened: map[string]int{}}
}

func (b *boards) add(root string, s *store.Store) { b.byRoot[root] = s }

func (b *boards) open(_ context.Context, path string) (Board, error) {
	for root, s := range b.byRoot {
		if path == root || filepath.Dir(path) == root {
			b.opened[root]++
			return Board{Root: root, Key: "key-" + filepath.Base(root), Store: s}, nil
		}
	}
	return Board{}, fmt.Errorf("%s: %w", path, ErrNotARepo)
}

func newWorkspace(t *testing.T) (*Workspace, *store.Store, *boards) {
	t.Helper()
	home := openStore(t, "WS-", columns)
	b := newBoards()
	return New(home, "/home/me/workspace", b.open), home, b
}

func TestCreateTicketWithoutRepoStaysOnHomeBoard(t *testing.T) {
	w, home, _ := newWorkspace(t)
	ctx := context.Background()

	got, err := w.CreateTicket(ctx, store.Ticket{Title: "plan", Status: "backlog"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if got.ID != "WS-01" || got.Repo != "" {
		t.Errorf("created %s repo %q, want WS-01 on the home board", got.ID, got.Repo)
	}
	if _, err := home.GetTicket(ctx, "WS-01"); err != nil {
		t.Errorf("home board does not hold WS-01: %v", err)
	}
}

func TestCreateTicketWithRepoLivesOnThatRepositorysBoard(t *testing.T) {
	w, home, b := newWorkspace(t)
	ctx := context.Background()
	freecode := openStore(t, "FRE-", columns)
	b.add("/src/freecode", freecode)

	got, err := w.CreateTicket(ctx, store.Ticket{Title: "fix x", Status: "backlog", Repo: "/src/freecode"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if got.ID != "FRE-01" || got.Repo != "/src/freecode" {
		t.Errorf("created %s repo %q, want FRE-01 in /src/freecode", got.ID, got.Repo)
	}
	if _, err := freecode.GetTicket(ctx, "FRE-01"); err != nil {
		t.Errorf("freecode's own board does not hold the ticket: %v", err)
	}
	if _, err := home.GetTicket(ctx, "FRE-01"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("home board holds a copy of FRE-01 (err %v); a ticket has one home", err)
	}
	linked, _ := home.ListLinkedRepos(ctx)
	if len(linked) != 1 || linked[0].Path != "/src/freecode" || linked[0].Key != "key-freecode" {
		t.Errorf("linked repos = %+v, want /src/freecode recorded", linked)
	}
}

func TestCreateTicketInASubdirectoryResolvesToTheRepositoryRoot(t *testing.T) {
	w, _, b := newWorkspace(t)
	b.add("/src/freecode", openStore(t, "FRE-", columns))

	got, err := w.CreateTicket(context.Background(), store.Ticket{Title: "x", Status: "backlog", Repo: "/src/freecode/apps"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if got.Repo != "/src/freecode" {
		t.Errorf("Repo = %q, want the repository root /src/freecode", got.Repo)
	}
}

func TestCreateTicketNamingTheHomeRepositoryStaysHome(t *testing.T) {
	home := openStore(t, "AGE-", columns)
	b := newBoards()
	b.add("/src/agent-board", home)
	w := New(home, "/src/agent-board", b.open)

	got, err := w.CreateTicket(context.Background(), store.Ticket{Title: "x", Status: "backlog", Repo: "/src/agent-board"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if got.ID != "AGE-01" || got.Repo != "" {
		t.Errorf("created %s repo %q, want AGE-01 with no foreign repo", got.ID, got.Repo)
	}
}

func TestCreateTicketOutsideAnyRepositoryFails(t *testing.T) {
	w, _, _ := newWorkspace(t)
	_, err := w.CreateTicket(context.Background(), store.Ticket{Title: "x", Status: "backlog", Repo: "/tmp/nowhere"})
	if !errors.Is(err, ErrNotARepo) {
		t.Errorf("err = %v, want ErrNotARepo", err)
	}
}

func TestLinkRefusesAPrefixTheWorkspaceAlreadyUses(t *testing.T) {
	w, _, b := newWorkspace(t)
	b.add("/src/agent-board", openStore(t, "AGE-", columns))
	b.add("/src/agent-orchestrator", openStore(t, "AGE-", columns))
	ctx := context.Background()

	if _, err := w.Link(ctx, "/src/agent-board"); err != nil {
		t.Fatalf("Link agent-board: %v", err)
	}
	if _, err := w.Link(ctx, "/src/agent-orchestrator"); !errors.Is(err, ErrPrefixClash) {
		t.Errorf("Link agent-orchestrator err = %v, want ErrPrefixClash", err)
	}
}

func TestLinkRefusesAPrefixThatClashesWithHome(t *testing.T) {
	w, _, b := newWorkspace(t)
	b.add("/src/web", openStore(t, "WS-", columns))
	if _, err := w.Link(context.Background(), "/src/web"); !errors.Is(err, ErrPrefixClash) {
		t.Errorf("err = %v, want ErrPrefixClash", err)
	}
}

func TestLinkRefusesABoardWithDifferentColumns(t *testing.T) {
	w, _, b := newWorkspace(t)
	b.add("/src/odd", openStore(t, "ODD-", []string{"todo", "done"}))
	if _, err := w.Link(context.Background(), "/src/odd"); !errors.Is(err, ErrColumnsDiffer) {
		t.Errorf("err = %v, want ErrColumnsDiffer", err)
	}
}

func TestLinkTwiceOpensTheBoardOnce(t *testing.T) {
	w, _, b := newWorkspace(t)
	b.add("/src/freecode", openStore(t, "FRE-", columns))
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := w.Link(ctx, "/src/freecode"); err != nil {
			t.Fatalf("Link #%d: %v", i, err)
		}
	}
	if b.opened["/src/freecode"] != 1 {
		t.Errorf("opened %d times, want 1", b.opened["/src/freecode"])
	}
}

func TestListTicketsMergesEveryBoardInCreationOrder(t *testing.T) {
	w, _, b := newWorkspace(t)
	b.add("/src/freecode", openStore(t, "FRE-", columns))
	ctx := context.Background()

	for _, in := range []store.Ticket{
		{Title: "a", Status: "backlog"},
		{Title: "b", Status: "backlog", Repo: "/src/freecode"},
		{Title: "c", Status: "review"},
		{Title: "d", Status: "backlog"},
	} {
		if _, err := w.CreateTicket(ctx, in); err != nil {
			t.Fatalf("CreateTicket %s: %v", in.Title, err)
		}
	}

	got, err := w.ListTickets(ctx, store.TicketFilters{Status: "backlog"})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	var titles []string
	for _, tk := range got {
		titles = append(titles, tk.Title)
	}
	if fmt.Sprint(titles) != "[a b d]" {
		t.Errorf("backlog = %v, want [a b d]", titles)
	}
	if got[1].Repo != "/src/freecode" {
		t.Errorf("b.Repo = %q, want /src/freecode", got[1].Repo)
	}
}

func TestTicketWritesReachTheOwningBoard(t *testing.T) {
	w, _, b := newWorkspace(t)
	freecode := openStore(t, "FRE-", columns)
	b.add("/src/freecode", freecode)
	ctx := context.Background()

	tk, err := w.CreateTicket(ctx, store.Ticket{Title: "x", Status: "backlog", Repo: "/src/freecode"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if err := w.MoveStatus(ctx, tk.ID, "in_progress"); err != nil {
		t.Fatalf("MoveStatus: %v", err)
	}
	if err := w.SetWorktreePath(ctx, tk.ID, "/wt/FRE-01"); err != nil {
		t.Fatalf("SetWorktreePath: %v", err)
	}
	if err := w.SetAgentActive(ctx, tk.ID, true); err != nil {
		t.Fatalf("SetAgentActive: %v", err)
	}

	direct, err := freecode.GetTicket(ctx, tk.ID)
	if err != nil {
		t.Fatalf("freecode.GetTicket: %v", err)
	}
	if direct.Status != "in_progress" || direct.WorktreePath != "/wt/FRE-01" || !direct.AgentActive {
		t.Errorf("freecode board sees %+v, want the workspace's writes", direct)
	}

	viaWorkspace, err := w.GetTicket(ctx, tk.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if viaWorkspace.Repo != "/src/freecode" {
		t.Errorf("GetTicket Repo = %q, want /src/freecode", viaWorkspace.Repo)
	}
}

func TestEditsMadeOnTheRepositorysOwnBoardShowInTheWorkspace(t *testing.T) {
	w, _, b := newWorkspace(t)
	freecode := openStore(t, "FRE-", columns)
	b.add("/src/freecode", freecode)
	ctx := context.Background()
	if _, err := w.Link(ctx, "/src/freecode"); err != nil {
		t.Fatalf("Link: %v", err)
	}

	own, err := freecode.CreateTicket(ctx, store.Ticket{Title: "made in freecode", Status: "backlog"})
	if err != nil {
		t.Fatalf("freecode.CreateTicket: %v", err)
	}

	got, err := w.GetTicket(ctx, own.ID)
	if err != nil {
		t.Fatalf("workspace GetTicket: %v", err)
	}
	if got.Title != "made in freecode" || got.Repo != "/src/freecode" {
		t.Errorf("workspace sees %+v", got)
	}
}

func TestDeleteForeignTicketAlsoPurgesTheWorkspacesRecords(t *testing.T) {
	w, home, b := newWorkspace(t)
	freecode := openStore(t, "FRE-", columns)
	b.add("/src/freecode", freecode)
	ctx := context.Background()

	tk, err := w.CreateTicket(ctx, store.Ticket{Title: "x", Status: "backlog", Repo: "/src/freecode"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if _, err := w.CreateSession(ctx, store.Session{TicketID: tk.ID, Agent: "claude", Status: "running"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := w.DeleteTicket(ctx, tk.ID); err != nil {
		t.Fatalf("DeleteTicket: %v", err)
	}
	if _, err := freecode.GetTicket(ctx, tk.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ticket still on freecode's board (err %v)", err)
	}
	if home.HasActiveSession(ctx, tk.ID) {
		t.Error("workspace still records a session for the deleted ticket")
	}
}

func TestSessionsLiveOnTheHomeBoard(t *testing.T) {
	w, home, b := newWorkspace(t)
	freecode := openStore(t, "FRE-", columns)
	b.add("/src/freecode", freecode)
	ctx := context.Background()

	tk, _ := w.CreateTicket(ctx, store.Ticket{Title: "x", Status: "backlog", Repo: "/src/freecode"})
	if _, err := w.CreateSession(ctx, store.Session{TicketID: tk.ID, Agent: "claude", Status: "running"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if !home.HasActiveSession(ctx, tk.ID) {
		t.Error("session not recorded on the home board")
	}
	if freecode.HasActiveSession(ctx, tk.ID) {
		t.Error("session recorded on freecode's board; the process running the agent owns it")
	}
}

func TestRestoreReopensLinkedRepositories(t *testing.T) {
	home := openStore(t, "WS-", columns)
	b := newBoards()
	freecode := openStore(t, "FRE-", columns)
	b.add("/src/freecode", freecode)
	ctx := context.Background()

	first := New(home, "/home/me/workspace", b.open)
	if _, err := first.CreateTicket(ctx, store.Ticket{Title: "x", Status: "backlog", Repo: "/src/freecode"}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	second := New(home, "/home/me/workspace", b.open)
	if err := second.Restore(ctx); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got, err := second.ListTickets(ctx, store.TicketFilters{})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(got) != 1 || got[0].ID != "FRE-01" {
		t.Errorf("after restore = %+v, want FRE-01 from freecode", got)
	}
}

func TestRestoreSkipsARepositoryThatIsGoneButKeepsTheRest(t *testing.T) {
	home := openStore(t, "WS-", columns)
	ctx := context.Background()
	if err := home.LinkRepo(ctx, store.LinkedRepo{Path: "/src/deleted", Key: "k"}); err != nil {
		t.Fatalf("LinkRepo: %v", err)
	}
	if err := home.LinkRepo(ctx, store.LinkedRepo{Path: "/src/freecode", Key: "key-freecode"}); err != nil {
		t.Fatalf("LinkRepo: %v", err)
	}
	b := newBoards()
	b.add("/src/freecode", openStore(t, "FRE-", columns))

	w := New(home, "/home/me/workspace", b.open)
	err := w.Restore(ctx)
	if !errors.Is(err, ErrNotARepo) {
		t.Errorf("Restore err = %v, want the missing repository reported", err)
	}
	if got := w.Repos(); len(got) != 1 || got[0] != "/src/freecode" {
		t.Errorf("Repos = %v, want [/src/freecode]", got)
	}
}
