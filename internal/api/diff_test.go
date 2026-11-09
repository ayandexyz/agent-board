package api_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ayandexyz/agent-board/internal/apitypes"
	"github.com/ayandexyz/agent-board/internal/git"
	"github.com/ayandexyz/agent-board/internal/orchestrator"
	"github.com/ayandexyz/agent-board/internal/store"
)

func (f *fakeBoard) TicketDiff(_ context.Context, id string) (orchestrator.TicketDiff, error) {
	if f.diffErr != nil {
		return orchestrator.TicketDiff{}, f.diffErr
	}
	if _, err := f.GetTicket(context.Background(), id); err != nil {
		return orchestrator.TicketDiff{}, err
	}
	return f.diff, nil
}

func TestTicketDiff(t *testing.T) {
	b := newFakeBoard()
	b.tickets["AGT-01"] = store.Ticket{ID: "AGT-01", Title: "t", Status: "review"}
	b.diff = orchestrator.TicketDiff{
		Base: "abc123",
		Files: []git.FileChange{
			{Path: "main.go", Added: 3, Deleted: 1},
			{Path: "logo.png", Binary: true, Untracked: true},
		},
		Patch: "diff --git a/main.go b/main.go",
	}
	ts, _ := newTestServer(t, b, nil)

	resp, body := do(t, ts, "GET", "/api/v1/tickets/AGT-01/diff", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var got apitypes.TicketDiff
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.TicketID != "AGT-01" || got.Base != "abc123" || got.Patch != "diff --git a/main.go b/main.go" {
		t.Errorf("diff = %+v", got)
	}
	if got.Added != 3 || got.Deleted != 1 {
		t.Errorf("totals = +%d -%d, want +3 -1", got.Added, got.Deleted)
	}
	want := []apitypes.FileChange{
		{Path: "main.go", Added: 3, Deleted: 1},
		{Path: "logo.png", Binary: true, Untracked: true},
	}
	if len(got.Files) != 2 || got.Files[0] != want[0] || got.Files[1] != want[1] {
		t.Errorf("files = %+v, want %+v", got.Files, want)
	}
}

func TestTicketDiffErrors(t *testing.T) {
	b := newFakeBoard()
	ts, _ := newTestServer(t, b, nil)

	if resp, _ := do(t, ts, "GET", "/api/v1/tickets/NOPE-1/diff", nil); resp.StatusCode != 404 {
		t.Errorf("unknown ticket status %d, want 404", resp.StatusCode)
	}

	b.diffErr = orchestrator.ErrNoRepository
	if resp, _ := do(t, ts, "GET", "/api/v1/tickets/AGT-01/diff", nil); resp.StatusCode != 409 {
		t.Errorf("no repository status %d, want 409", resp.StatusCode)
	}
}
