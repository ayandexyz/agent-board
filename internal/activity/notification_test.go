package activity

import (
	"strings"
	"testing"
)

// An agent that waits on a question it asked itself reports a "question"
// notification whose message is the question. The alert quotes it.
func TestQuestionNotificationNeedsInputAndQuotesTheMessage(t *testing.T) {
	r := New()
	var gotTitle, gotBody string
	r.Notify = func(title, body string) { gotTitle, gotBody = title, body }

	r.apply("SES-01", "", decodePayload(t, `{"hook_event_name":"PreToolUse","tool_name":"question","tool_input":{"questions":[]}}`))
	if !r.apply("SES-01", "", decodePayload(t, `{"hook_event_name":"Notification","notification_type":"question","message":"Which database?  (+1 more)"}`)) {
		t.Fatal("question notification was not applied")
	}
	if got := r.StateOf("SES-01"); got != StateWaitingInput {
		t.Errorf("state = %q, want %q", got, StateWaitingInput)
	}
	if !strings.Contains(gotTitle, "needs your answer") {
		t.Errorf("title = %q", gotTitle)
	}
	if gotBody != "Which database? (+1 more)" {
		t.Errorf("body = %q, want the message", gotBody)
	}
}

// A notification with neither tool input nor message must not erase what the
// board already knew about the tool that is waiting.
func TestBareNotificationKeepsTheToolDetail(t *testing.T) {
	r := New()
	var gotBody string
	r.Notify = func(_, body string) { gotBody = body }

	r.apply("SES-01", "", decodePayload(t, `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"make test"}}`))
	r.apply("SES-01", "", decodePayload(t, `{"hook_event_name":"Notification","notification_type":"agent_needs_input"}`))
	if gotBody != "Bash" {
		t.Errorf("body = %q, want the tool name", gotBody)
	}
}
