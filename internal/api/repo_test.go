package api_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ayandexyz/agent-board/internal/apitypes"
	"github.com/ayandexyz/agent-board/internal/orchestrator"
	"github.com/ayandexyz/agent-board/internal/store"
	"github.com/ayandexyz/agent-board/internal/workspace"
)

func TestCreateTicketPassesTheRepositoryAndReportsIt(t *testing.T) {
	b := newFakeBoard()
	ts, _ := newTestServer(t, b, nil)

	resp, body := do(t, ts, "POST", "/api/v1/tickets", apitypes.CreateTicket{Title: "fix x", Repo: "/src/freecode"})
	if resp.StatusCode != 201 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var got apitypes.Ticket
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if b.lastCreateRepo != "/src/freecode" || got.Repo != "/src/freecode" {
		t.Errorf("board got repo %q, response repo %q; want /src/freecode both", b.lastCreateRepo, got.Repo)
	}
}

func TestMoveTicketToRepository(t *testing.T) {
	b := newFakeBoard()
	b.tickets["AGT-01"] = store.Ticket{ID: "AGT-01", Title: "t", Status: "backlog"}
	ts, _ := newTestServer(t, b, nil)

	resp, body := do(t, ts, "POST", "/api/v1/tickets/AGT-01/repo", apitypes.SetRepo{Repo: "/src/freecode"})
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var got apitypes.Ticket
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "FRE-01" || got.Repo != "/src/freecode" {
		t.Errorf("moved ticket = %+v, want FRE-01 in /src/freecode", got)
	}
}

func TestRepositoryErrorsMapToStatusCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{workspace.ErrNotARepo, 400},
		{orchestrator.ErrNoWorkspace, 400},
		{workspace.ErrPrefixClash, 409},
		{workspace.ErrColumnsDiffer, 409},
		{orchestrator.ErrRepoLocked, 409},
	} {
		b := newFakeBoard()
		b.tickets["AGT-01"] = store.Ticket{ID: "AGT-01", Title: "t", Status: "backlog"}
		b.repoErr = fmt.Errorf("wrapped: %w", tc.err)
		ts, _ := newTestServer(t, b, nil)

		resp, _ := do(t, ts, "POST", "/api/v1/tickets/AGT-01/repo", apitypes.SetRepo{Repo: "/x"})
		if resp.StatusCode != tc.want {
			t.Errorf("%v: status %d, want %d", tc.err, resp.StatusCode, tc.want)
		}
	}
}
