package activity

import (
	"bytes"
	"net/http"
	"testing"
)

func post(t *testing.T, base, session, launch, body string) {
	t.Helper()
	req, err := http.NewRequest("POST", base+"/hooks/"+session, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if launch != "" {
		req.Header.Set("X-Agentboard-Launch", launch)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func newRunning(t *testing.T) (*Receiver, string, chan [2]string) {
	t.Helper()
	alerts := make(chan [2]string, 8)
	r := New()
	r.Notify = func(title, body string) { alerts <- [2]string{title, body} }
	base, err := r.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r, base, alerts
}

func TestReceiverTracksStates(t *testing.T) {
	r, base, alerts := newRunning(t)

	post(t, base, "s1", "", `{"hook_event_name":"SessionStart"}`)
	if got := r.StateOf("s1"); got != StateActive {
		t.Fatalf("state = %q, want active", got)
	}

	post(t, base, "s1", "", `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"rm -rf build"}}`)
	if got := r.StateOf("s1"); got != StateBlocked {
		t.Fatalf("state = %q, want blocked", got)
	}
	if len(r.Waiting()) != 1 {
		t.Fatalf("Waiting() = %v, want one session", r.Waiting())
	}
	select {
	case a := <-alerts:
		if a[0] != "s1 needs approval" {
			t.Errorf("title = %q", a[0])
		}
	default:
		t.Error("no notification fired on entering blocked")
	}

	// Parallel tool traffic must not clear an open permission dialog.
	post(t, base, "s1", "", `{"hook_event_name":"PreToolUse","tool_name":"Read"}`)
	if got := r.StateOf("s1"); got != StateBlocked {
		t.Fatalf("PreToolUse cleared the block: %q", got)
	}

	post(t, base, "s1", "", `{"hook_event_name":"PostToolUse","tool_name":"Bash"}`)
	if got := r.StateOf("s1"); got != StateActive {
		t.Fatalf("state = %q, want active after PostToolUse", got)
	}

	post(t, base, "s1", "", `{"hook_event_name":"Stop"}`)
	if got := r.StateOf("s1"); got != StateIdle {
		t.Fatalf("state = %q, want idle", got)
	}
	if len(r.Waiting()) != 0 {
		t.Error("idle session still listed as waiting")
	}
}

func TestNotifiesOnceOnEntry(t *testing.T) {
	_, base, alerts := newRunning(t)

	post(t, base, "s1", "", `{"hook_event_name":"Notification","notification_type":"agent_needs_input"}`)
	post(t, base, "s1", "", `{"hook_event_name":"Notification","notification_type":"permission_prompt"}`)
	if len(alerts) != 1 {
		t.Fatalf("fired %d notifications, want 1 (escalation must not re-notify)", len(alerts))
	}
}

func TestDropsStaleLaunch(t *testing.T) {
	r, base, _ := newRunning(t)
	r.Launch("s1", "L2")

	post(t, base, "s1", "L1", `{"hook_event_name":"PermissionRequest"}`)
	if got := r.StateOf("s1"); got != "" {
		t.Fatalf("stale launch was applied: %q", got)
	}
	post(t, base, "s1", "L2", `{"hook_event_name":"PermissionRequest"}`)
	if got := r.StateOf("s1"); got != StateBlocked {
		t.Fatalf("current launch dropped: %q", got)
	}
}

func TestSessionEndReason(t *testing.T) {
	r, base, _ := newRunning(t)
	post(t, base, "s1", "", `{"hook_event_name":"SessionEnd","reason":"clear"}`)
	if got := r.StateOf("s1"); got != StateIdle {
		t.Fatalf("state = %q, want idle for reason=clear", got)
	}
	post(t, base, "s1", "", `{"hook_event_name":"SessionEnd","reason":"exit"}`)
	if got := r.StateOf("s1"); got != StateExited {
		t.Fatalf("state = %q, want exited", got)
	}
}

func TestLabelUsedInNotification(t *testing.T) {
	r, base, alerts := newRunning(t)
	r.Label = func(string) string { return "AGT-03" }
	post(t, base, "s1", "", `{"hook_event_name":"Notification","notification_type":"agent_needs_input"}`)
	a := <-alerts
	if a[0] != "AGT-03 needs your answer" {
		t.Errorf("title = %q", a[0])
	}
}

func TestUnknownEventIgnored(t *testing.T) {
	r, base, _ := newRunning(t)
	post(t, base, "s1", "", `{"hook_event_name":"PreCompact"}`)
	if got := r.StateOf("s1"); got != "" {
		t.Fatalf("unknown event created state %q", got)
	}
}
