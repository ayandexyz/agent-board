package activity

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func postRaw(t *testing.T, base, sessionID, body string) string {
	t.Helper()
	req, err := http.NewRequest("POST", base+"/hooks/"+sessionID, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return string(out)
}

func TestPermissionHeldOpenUntilResolved(t *testing.T) {
	r, base, _ := newRunning(t)
	r.PermissionTimeout = 5 * time.Second
	r.Launch("s1", "L1", NestedDecision)

	replies := make(chan string, 1)
	go func() {
		replies <- postRaw(t, base, "s1", `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"rm -rf build"}}`)
	}()

	deadline := time.After(2 * time.Second)
	for !r.Resolve("s1", "allow") {
		select {
		case <-deadline:
			t.Fatal("request never parked")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	select {
	case got := <-replies:
		want := `{"hookSpecificOutput":{"decision":{"behavior":"allow"},"hookEventName":"PermissionRequest"}}`
		if got != want {
			t.Fatalf("reply = %s, want %s", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reply not delivered after Resolve")
	}
}

func TestPermissionTimesOutWithNoDecision(t *testing.T) {
	r, base, _ := newRunning(t)
	r.PermissionTimeout = 30 * time.Millisecond

	if got := postRaw(t, base, "s1", `{"hook_event_name":"PermissionRequest","tool_name":"Bash"}`); got != "{}" {
		t.Fatalf("timeout reply = %s, want {}", got)
	}
	if r.Resolve("s1", "allow") {
		t.Fatal("request still parked after timeout")
	}
}

func TestPermissionReplyShape(t *testing.T) {
	if got := string(permissionReply(NestedDecision, "allow")); got != `{"hookSpecificOutput":{"decision":{"behavior":"allow"},"hookEventName":"PermissionRequest"}}` {
		t.Fatalf("nested reply = %s", got)
	}
	if got := string(permissionReply(NestedDecision, "")); got != "{}" {
		t.Fatalf("empty decision = %s, want {}", got)
	}
	// An agent that cannot be answered over the wire gets no decision at all.
	if got := string(permissionReply(nil, "allow")); got != "{}" {
		t.Fatalf("codecless reply = %s, want {}", got)
	}
}

func TestPermissionAlertIsDecidable(t *testing.T) {
	r, base, _ := newRunning(t)
	r.PermissionTimeout = 200 * time.Millisecond
	alerts, _ := r.Subscribe()
	done := make(chan string, 1)
	go func() { done <- postRaw(t, base, "s1", `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"ls"}}`) }()
	defer func() { <-done }()

	select {
	case a := <-alerts:
		if !a.Decidable {
			t.Fatal("permission alert not marked decidable")
		}
		if a.Body != "Bash: ls" {
			t.Fatalf("body = %q", a.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no alert")
	}
}

// A question is not an approval. Claude Code delivers AskUserQuestion as a
// PermissionRequest, but allowing it would only let the agent ask — the answer
// is still typed in the pane, exactly as it is for OpenCode and FreeCode. So
// the board must not hold the hook open pretending a decision is coming, and
// must not raise an approve/deny modal for it.
func TestQuestionPermissionIsNotHeldForADecision(t *testing.T) {
	r, base, _ := newRunning(t)
	r.PermissionTimeout = 10 * time.Second
	alerts, _ := r.Subscribe()

	replied := make(chan string, 1)
	go func() {
		replied <- postRaw(t, base, "s1", `{"hook_event_name":"PermissionRequest","tool_name":"AskUserQuestion",`+
			`"tool_input":{"questions":[{"question":"Which capital city is on two continents?"}]}}`)
	}()

	select {
	case got := <-replied:
		if got != "{}" {
			t.Fatalf("reply = %s, want {} so the agent asks in its own pane", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("question was held open waiting on a decision nobody can make")
	}

	select {
	case a := <-alerts:
		if a.State != StateWaitingInput {
			t.Errorf("state = %q, want %q", a.State, StateWaitingInput)
		}
		if a.Decidable {
			t.Error("question alert marked decidable; the TUI would raise an approval modal")
		}
		if !strings.Contains(a.Body, "two continents") {
			t.Errorf("body = %q, want the question itself", a.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no alert raised for the question")
	}
}

func TestWaitingReportsWhetherDecidable(t *testing.T) {
	r, base, _ := newRunning(t)
	r.PermissionTimeout = time.Second

	post(t, base, "q1", "", `{"hook_event_name":"PermissionRequest","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Which?"}]}}`)
	held := make(chan string, 1)
	go func() {
		held <- postRaw(t, base, "p1", `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"ls"}}`)
	}()

	deadline := time.Now().Add(time.Second)
	for r.StateOf("p1") != StateBlocked {
		if time.Now().After(deadline) {
			t.Fatal("p1 never blocked")
		}
		time.Sleep(5 * time.Millisecond)
	}
	byID := map[string]Session{}
	for _, s := range r.Waiting() {
		byID[s.ID] = s
	}
	if !byID["p1"].Decidable {
		t.Fatalf("held permission not decidable: %+v", byID["p1"])
	}
	if byID["q1"].Decidable {
		t.Fatalf("question reported decidable: %+v", byID["q1"])
	}
	r.Resolve("p1", "deny")
	<-held
}
