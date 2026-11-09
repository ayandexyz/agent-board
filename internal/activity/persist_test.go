package activity

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ayandexyz/agent-board/internal/store"
)

type recordedRequest struct {
	id       string
	decision string
}

type fakeStore struct {
	mu        sync.Mutex
	fail      bool
	launches  map[string]string
	states    []string
	created   []store.PendingRequest
	resolved  []recordedRequest
	abandoned int
	activity  []store.SessionActivity
}

func newFakeStore() *fakeStore {
	return &fakeStore{launches: map[string]string{}}
}

func (f *fakeStore) err(op string) error {
	if f.fail {
		return fmt.Errorf("fake %s: %w", op, errors.New("boom"))
	}
	return nil
}

func (f *fakeStore) SetLaunchID(_ context.Context, sessionID, launchID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.launches[sessionID] = launchID
	return f.err("SetLaunchID")
}

func (f *fakeStore) SetActivity(_ context.Context, sessionID, state string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states = append(f.states, sessionID+":"+state)
	return f.err("SetActivity")
}

func (f *fakeStore) ListActivity(_ context.Context) ([]store.SessionActivity, error) {
	return f.activity, f.err("ListActivity")
}

func (f *fakeStore) CreatePendingRequest(_ context.Context, r store.PendingRequest) (store.PendingRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.err("CreatePendingRequest"); err != nil {
		return store.PendingRequest{}, err
	}
	r.ID = fmt.Sprintf("REQ-%02d", len(f.created)+1)
	f.created = append(f.created, r)
	return r, nil
}

func (f *fakeStore) ResolvePendingRequest(_ context.Context, id, decision string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolved = append(f.resolved, recordedRequest{id, decision})
	return f.err("ResolvePendingRequest")
}

func (f *fakeStore) AbandonPendingRequests(_ context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.abandoned++
	return 0, f.err("AbandonPendingRequests")
}

func (f *fakeStore) snapshot() ([]string, []store.PendingRequest, []recordedRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.states...), append([]store.PendingRequest(nil), f.created...), append([]recordedRequest(nil), f.resolved...)
}

func newPersisting(t *testing.T) (*Receiver, string, *fakeStore) {
	t.Helper()
	r, base, _ := newRunning(t)
	fs := newFakeStore()
	r.Store = fs
	return r, base, fs
}

func TestLaunchPersistsLaunchID(t *testing.T) {
	r, _, fs := newPersisting(t)
	r.Launch("SES-01", "L1", nil)
	if fs.launches["SES-01"] != "L1" {
		t.Fatalf("launch id not persisted: %v", fs.launches)
	}
}

func TestEveryAcceptedSignalIsPersisted(t *testing.T) {
	r, base, fs := newPersisting(t)
	r.Launch("SES-01", "L1", nil)

	post(t, base, "SES-01", "L1", `{"hook_event_name":"SessionStart"}`)
	post(t, base, "SES-01", "L1", `{"hook_event_name":"Stop"}`)
	post(t, base, "SES-01", "L0", `{"hook_event_name":"SessionStart"}`)
	post(t, base, "SES-01", "L1", `{"hook_event_name":"Nonsense"}`)

	states, _, _ := fs.snapshot()
	want := []string{"SES-01:active", "SES-01:idle"}
	if fmt.Sprint(states) != fmt.Sprint(want) {
		t.Fatalf("persisted states = %v, want %v (stale launch and unknown events must not write)", states, want)
	}
}

func TestHeldPermissionIsRecordedAndResolved(t *testing.T) {
	r, base, fs := newPersisting(t)
	r.PermissionTimeout = time.Second

	done := make(chan string, 1)
	go func() {
		done <- postRaw(t, base, "SES-01", `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"rm -rf build"}}`)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		_, created, _ := fs.snapshot()
		if len(created) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("request was never recorded while held")
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, created, _ := fs.snapshot()
	c := created[0]
	if c.SessionID != "SES-01" || c.Kind != "permission" || c.ToolName != "Bash" || c.ToolInput != `{"command":"rm -rf build"}` {
		t.Fatalf("recorded request = %+v", c)
	}

	if !r.Resolve("SES-01", "allow") {
		t.Fatal("nothing was waiting")
	}
	<-done
	_, _, resolved := fs.snapshot()
	if len(resolved) != 1 || resolved[0].id != "REQ-01" || resolved[0].decision != "allow" {
		t.Fatalf("resolved = %+v, want REQ-01 allow", resolved)
	}
}

func TestTimedOutPermissionIsResolvedAsTimeout(t *testing.T) {
	r, base, fs := newPersisting(t)
	r.PermissionTimeout = 20 * time.Millisecond

	postRaw(t, base, "SES-01", `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"ls"}}`)

	_, _, resolved := fs.snapshot()
	if len(resolved) != 1 || resolved[0].decision != DecisionTimeout {
		t.Fatalf("resolved = %+v, want one %q", resolved, DecisionTimeout)
	}
}

func TestQuestionIsNotRecordedAsRequest(t *testing.T) {
	_, base, fs := newPersisting(t)
	post(t, base, "SES-01", "", `{"hook_event_name":"PermissionRequest","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Which?"}]}}`)
	states, created, _ := fs.snapshot()
	if len(created) != 0 {
		t.Fatalf("a question was recorded as a held request: %+v", created)
	}
	if fmt.Sprint(states) != "[SES-01:waiting_input]" {
		t.Fatalf("states = %v", states)
	}
}

func TestStoreFailureDoesNotBreakTracking(t *testing.T) {
	r, base, fs := newPersisting(t)
	fs.fail = true
	r.PermissionTimeout = 20 * time.Millisecond

	r.Launch("SES-01", "L1", nil)
	post(t, base, "SES-01", "L1", `{"hook_event_name":"SessionStart"}`)
	if r.StateOf("SES-01") != StateActive {
		t.Fatal("state lost because the store failed")
	}
	postRaw(t, base, "SES-01", `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{}}`)
	if r.StateOf("SES-01") != StateBlocked {
		t.Fatal("permission not tracked because the store failed")
	}
}

func TestRestoreLoadsLiveSessionsAndAbandonsOpenRequests(t *testing.T) {
	r, base, fs := newPersisting(t)
	at := time.Now().Add(-time.Minute)
	fs.activity = []store.SessionActivity{
		{SessionID: "SES-01", TicketID: "AGT-01", State: "blocked", ActivityAt: at, LaunchID: "L1"},
		{SessionID: "SES-02", TicketID: "AGT-02"},
	}

	if err := r.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fs.abandoned != 1 {
		t.Fatalf("open requests were not abandoned (calls=%d)", fs.abandoned)
	}
	if got := r.StateOf("SES-01"); got != StateBlocked {
		t.Fatalf("SES-01 state = %q, want blocked", got)
	}
	if got := r.StateOf("SES-02"); got != "" {
		t.Fatalf("SES-02 state = %q, want unreported", got)
	}
	waiting := r.Waiting()
	if len(waiting) != 1 || waiting[0].ID != "SES-01" {
		t.Fatalf("Waiting() = %+v", waiting)
	}

	post(t, base, "SES-01", "L0", `{"hook_event_name":"Stop"}`)
	if got := r.StateOf("SES-01"); got != StateBlocked {
		t.Fatalf("a hook from an older launch was accepted after restore: %q", got)
	}
	post(t, base, "SES-01", "L1", `{"hook_event_name":"Stop"}`)
	if got := r.StateOf("SES-01"); got != StateIdle {
		t.Fatalf("the restored launch id did not admit its own hooks: %q", got)
	}
}

func TestRestoreWithoutStoreIsNoop(t *testing.T) {
	r, _, _ := newRunning(t)
	if err := r.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreReportsStoreErrors(t *testing.T) {
	r, _, fs := newPersisting(t)
	fs.fail = true
	if err := r.Restore(context.Background()); err == nil {
		t.Fatal("expected an error from a failing store")
	}
}
