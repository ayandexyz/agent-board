package orchestrator_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ayan-de/agent-board/internal/git"
	"github.com/ayan-de/agent-board/internal/orchestrator"
	"github.com/ayan-de/agent-board/internal/store"
)

type diffingWorktrees struct {
	fakeWorktrees
	diff       orchestrator.TicketDiff
	diffTicket store.Ticket
}

func (f *diffingWorktrees) Diff(_ context.Context, t store.Ticket) (orchestrator.TicketDiff, error) {
	f.diffTicket = t
	return f.diff, nil
}

func TestTicketDiffAsksWorktreesWithStoredTicket(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01", WorktreePath: "/wt/AGT-01"}}
	want := orchestrator.TicketDiff{Base: "abc", Files: []git.FileChange{{Path: "a.go", Added: 1}}, Patch: "P"}
	wt := &diffingWorktrees{diff: want}

	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})
	svc.SetWorktrees(wt)

	got, err := svc.TicketDiff(context.Background(), "AGT-01")
	if err != nil {
		t.Fatalf("TicketDiff: %v", err)
	}
	if got.Base != want.Base || got.Patch != want.Patch || len(got.Files) != 1 {
		t.Errorf("TicketDiff = %+v, want %+v", got, want)
	}
	if wt.diffTicket.WorktreePath != "/wt/AGT-01" {
		t.Errorf("Diff got ticket %+v, want the stored ticket", wt.diffTicket)
	}
}

func TestTicketDiffWithoutRepository(t *testing.T) {
	fs := &fakeStore{ticket: store.Ticket{ID: "AGT-01"}}
	svc := orchestrator.NewService(fs, &fakeLLMClient{}, nil, fakeCtx{})

	if _, err := svc.TicketDiff(context.Background(), "AGT-01"); !errors.Is(err, orchestrator.ErrNoRepository) {
		t.Errorf("TicketDiff err = %v, want ErrNoRepository", err)
	}
}
