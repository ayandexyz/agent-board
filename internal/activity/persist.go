package activity

import (
	"context"
	"fmt"
	"time"

	"github.com/ayandexyz/agent-board/internal/store"
)

// Store is what the receiver writes through to. Every write is best effort:
// a hook must never stall on the database, so failures are dropped and the
// in-memory state stays authoritative for this process.
type Store interface {
	SetLaunchID(ctx context.Context, sessionID, launchID string) error
	SetActivity(ctx context.Context, sessionID, state string, at time.Time) error
	ListActivity(ctx context.Context) ([]store.SessionActivity, error)
	CreatePendingRequest(ctx context.Context, r store.PendingRequest) (store.PendingRequest, error)
	ResolvePendingRequest(ctx context.Context, id, decision string) error
	AbandonPendingRequests(ctx context.Context) (int, error)
}

// Decisions recorded on a request that no human answered.
const (
	// DecisionTimeout: the board's own hold expired; the agent fell back to
	// its pane picker.
	DecisionTimeout = "timeout"
	// DecisionDropped: the agent hung up before anyone decided.
	DecisionDropped = "dropped"
	// DecisionPane: the board explicitly handed the choice back to the pane.
	DecisionPane = "pane"
)

// Restore loads the last known state of every live session from the store,
// so a restarted board shows what its agents were doing before it went away.
// Open requests are closed as abandoned: whatever hook was holding them died
// with the previous process.
func (r *Receiver) Restore(ctx context.Context) error {
	if r.Store == nil {
		return nil
	}
	if _, err := r.Store.AbandonPendingRequests(ctx); err != nil {
		return fmt.Errorf("activity.Restore: %w", err)
	}
	live, err := r.Store.ListActivity(ctx)
	if err != nil {
		return fmt.Errorf("activity.Restore: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range live {
		if a.LaunchID != "" {
			r.launches[a.SessionID] = a.LaunchID
		}
		if a.State != "" {
			r.sessions[a.SessionID] = &Session{ID: a.SessionID, State: State(a.State)}
		}
	}
	return nil
}

func (r *Receiver) persistLaunch(sessionID, launchID string) {
	if r.Store == nil {
		return
	}
	_ = r.Store.SetLaunchID(context.Background(), sessionID, launchID)
}

func (r *Receiver) persistState(sessionID string, s State) {
	if r.Store == nil {
		return
	}
	_ = r.Store.SetActivity(context.Background(), sessionID, string(s), time.Now())
}

// openRequest records a held permission request and returns its id, or ""
// when nothing was recorded.
func (r *Receiver) openRequest(sessionID string, p payload) string {
	if r.Store == nil {
		return ""
	}
	rec, err := r.Store.CreatePendingRequest(context.Background(), store.PendingRequest{
		SessionID: sessionID,
		Kind:      "permission",
		ToolName:  p.ToolName,
		ToolInput: string(p.ToolInput),
	})
	if err != nil {
		return ""
	}
	return rec.ID
}

func (r *Receiver) closeRequest(id, decision string) {
	if r.Store == nil || id == "" {
		return
	}
	_ = r.Store.ResolvePendingRequest(context.Background(), id, decision)
}
