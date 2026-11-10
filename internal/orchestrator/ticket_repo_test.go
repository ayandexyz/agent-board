package orchestrator_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ayandexyz/agent-board/internal/orchestrator"
	"github.com/ayandexyz/agent-board/internal/store"
	"github.com/ayandexyz/agent-board/internal/workspace"
)

var boardColumns = []string{"backlog", "in_progress", "review", "done"}

func openBoard(t *testing.T, prefix string) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "board.db"), boardColumns, prefix)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func workspaceService(t *testing.T, repos map[string]*store.Store) (*orchestrator.Service, *store.Store) {
	t.Helper()
	home := openBoard(t, "WS-")
	open := func(_ context.Context, path string) (workspace.Board, error) {
		if s, ok := repos[path]; ok {
			return workspace.Board{Root: path, Key: filepath.Base(path), Store: s}, nil
		}
		return workspace.Board{}, fmt.Errorf("%s: %w", path, workspace.ErrNotARepo)
	}
	ws := workspace.New(home, "", open)
	return orchestrator.NewService(ws, &fakeLLMClient{}, nil, fakeCtx{}), home
}

func TestCreateTicketWithRepoLandsOnThatRepositorysBoard(t *testing.T) {
	freecode := openBoard(t, "FRE-")
	svc, _ := workspaceService(t, map[string]*store.Store{"/src/freecode": freecode})

	got, err := svc.CreateTicket(context.Background(), orchestrator.CreateTicketInput{Title: "fix x", Repo: "/src/freecode"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if got.ID != "FRE-01" || got.Repo != "/src/freecode" || got.Status != "backlog" {
		t.Errorf("created %+v, want FRE-01 in freecode's first column", got)
	}
}

func TestSetTicketRepoMovesTheTicketToThatRepositorysBoard(t *testing.T) {
	freecode := openBoard(t, "FRE-")
	svc, home := workspaceService(t, map[string]*store.Store{"/src/freecode": freecode})
	ctx := context.Background()

	orig, err := svc.CreateTicket(ctx, orchestrator.CreateTicketInput{Title: "fix x", Prompt: "do it", Priority: "high", Agent: "claude"})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	moved, err := svc.SetTicketRepo(ctx, orig.ID, "/src/freecode")
	if err != nil {
		t.Fatalf("SetTicketRepo: %v", err)
	}
	if moved.ID != "FRE-01" || moved.Repo != "/src/freecode" {
		t.Errorf("moved to %s in %q, want FRE-01 in /src/freecode", moved.ID, moved.Repo)
	}
	if moved.Title != "fix x" || moved.Prompt != "do it" || moved.Priority != "high" || moved.Agent != "claude" {
		t.Errorf("moved ticket lost fields: %+v", moved)
	}
	if _, err := home.GetTicket(ctx, orig.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("original %s still on the home board (err %v)", orig.ID, err)
	}
	if _, err := freecode.GetTicket(ctx, "FRE-01"); err != nil {
		t.Errorf("freecode board does not hold the moved ticket: %v", err)
	}
}

func TestSetTicketRepoToItsCurrentRepositoryChangesNothing(t *testing.T) {
	freecode := openBoard(t, "FRE-")
	svc, _ := workspaceService(t, map[string]*store.Store{"/src/freecode": freecode})
	ctx := context.Background()

	orig, _ := svc.CreateTicket(ctx, orchestrator.CreateTicketInput{Title: "x", Repo: "/src/freecode"})
	got, err := svc.SetTicketRepo(ctx, orig.ID, "/src/freecode")
	if err != nil {
		t.Fatalf("SetTicketRepo: %v", err)
	}
	if got.ID != orig.ID {
		t.Errorf("ID = %s, want %s unchanged", got.ID, orig.ID)
	}
}

func TestSetTicketRepoRefusesOnceWorkHasStarted(t *testing.T) {
	freecode := openBoard(t, "FRE-")
	svc, home := workspaceService(t, map[string]*store.Store{"/src/freecode": freecode})
	ctx := context.Background()

	orig, _ := svc.CreateTicket(ctx, orchestrator.CreateTicketInput{Title: "x"})
	if err := home.SetWorktreePath(ctx, orig.ID, "/wt/WS-01"); err != nil {
		t.Fatalf("SetWorktreePath: %v", err)
	}

	_, err := svc.SetTicketRepo(ctx, orig.ID, "/src/freecode")
	if !errors.Is(err, orchestrator.ErrRepoLocked) {
		t.Errorf("err = %v, want ErrRepoLocked", err)
	}
	if _, err := home.GetTicket(ctx, orig.ID); err != nil {
		t.Errorf("refused move still removed the ticket: %v", err)
	}
}

func TestSetTicketRepoOnABoardWithoutWorkspaceSupportFails(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", Title: "x", Status: "backlog"}}
	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})

	_, err := svc.SetTicketRepo(context.Background(), "AGT-01", "/src/freecode")
	if !errors.Is(err, orchestrator.ErrNoWorkspace) {
		t.Errorf("err = %v, want ErrNoWorkspace", err)
	}
}
