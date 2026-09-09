package activity

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenCodeConfigWritesPluginAndConfig(t *testing.T) {
	dir := t.TempDir()
	path, err := OpenCodeConfig(dir)
	if err != nil {
		t.Fatalf("OpenCodeConfig() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg struct {
		Plugin []string `json:"plugin"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("config is not json: %v", err)
	}
	if len(cfg.Plugin) != 1 || !strings.HasPrefix(cfg.Plugin[0], "file://") {
		t.Fatalf("config plugin = %v, want one file:// entry", cfg.Plugin)
	}
	js := strings.TrimPrefix(cfg.Plugin[0], "file://")
	if filepath.Dir(js) != dir {
		t.Errorf("plugin written to %s, want inside %s", js, dir)
	}
	if _, err := os.Stat(js); err != nil {
		t.Fatalf("plugin file missing: %v", err)
	}
}

func TestOpenCodeConfigIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	first, err := OpenCodeConfig(dir)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := OpenCodeConfig(dir)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first != second {
		t.Errorf("path changed between calls: %s then %s", first, second)
	}
}

// The plugin is shipped as source, so a syntax error would only surface in a
// user's pane. Parse it here with whatever JS runtime is on PATH.
func TestOpenCodePluginParses(t *testing.T) {
	bin, err := exec.LookPath("bun")
	if err != nil {
		if bin, err = exec.LookPath("node"); err != nil {
			t.Skip("no bun or node on PATH")
		}
	}
	dir := t.TempDir()
	if _, err := OpenCodeConfig(dir); err != nil {
		t.Fatalf("OpenCodeConfig() error = %v", err)
	}
	out, err := exec.Command(bin, "--check", filepath.Join(dir, openCodePluginFile)).CombinedOutput()
	if err != nil {
		t.Fatalf("%s --check failed: %v\n%s", bin, err, out)
	}
}

// The plugin forwards OpenCode's question.asked as an AskUserQuestion
// notification, carrying that event's own `questions` array verbatim.
func TestOpenCodeQuestionNotificationNeedsInput(t *testing.T) {
	r := New()
	var gotTitle, gotBody string
	r.Notify = func(title, body string) { gotTitle, gotBody = title, body }

	body := `{"hook_event_name":"Notification","notification_type":"agent_needs_input",` +
		`"tool_name":"AskUserQuestion","tool_input":{"questions":[` +
		`{"question":"Which capital city is on two continents?","header":"Quiz","options":[]}]}}`
	if !r.apply("SES-01", "", decodePayload(t, body)) {
		t.Fatal("question notification was not applied")
	}
	if got := r.StateOf("SES-01"); got != StateWaitingInput {
		t.Errorf("state = %q, want %q", got, StateWaitingInput)
	}
	if !strings.Contains(gotTitle, "needs your answer") {
		t.Errorf("title = %q", gotTitle)
	}
	if gotBody != "Which capital city is on two continents?" {
		t.Errorf("body = %q, want the question itself", gotBody)
	}
}

func decodePayload(t *testing.T, body string) payload {
	t.Helper()
	var p payload
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return p
}

// End to end for the half that broke: OpenCode raises question.asked, and the
// board must end up with a needs-input alert quoting the question. Driving the
// real plugin file keeps the two halves from drifting apart again.
func TestOpenCodePluginQuestionReachesTheBoard(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("no bun on PATH")
	}
	dir := t.TempDir()
	if _, err := OpenCodeConfig(dir); err != nil {
		t.Fatalf("OpenCodeConfig() error = %v", err)
	}

	r := New()
	// Notify is a plain field read by the serving goroutine, so it has to be
	// set before Start hands the receiver to net/http.
	notified := make(chan string, 4)
	r.Notify = func(title, body string) { notified <- body }
	base, err := r.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer r.Close()
	r.Launch("SES-Q", "L9", "opencode")

	const event = `{"type":"question.asked","properties":{"id":"que_1","sessionID":"ses_1",` +
		`"questions":[{"question":"Which capital city is on two continents?","header":"Quiz","options":[]}]}}`
	cmd := exec.Command(bun, "opencode_plugin_probe.mjs", filepath.Join(dir, openCodePluginFile), event)
	cmd.Env = append(os.Environ(),
		"AGENTBOARD_HOOK_URL="+base,
		"AGENTBOARD_SESSION_ID=SES-Q",
		"AGENTBOARD_LAUNCH_ID=L9",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}

	select {
	case body := <-notified:
		if body != "Which capital city is on two continents?" {
			t.Errorf("notification body = %q, want the question itself", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no notification raised for question.asked")
	}
	if got := r.StateOf("SES-Q"); got != StateWaitingInput {
		t.Errorf("state = %q, want %q", got, StateWaitingInput)
	}
}
