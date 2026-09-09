package activity

import (
	"encoding/json"
	"testing"
)

type claudeSettings struct {
	Hooks map[string][]struct {
		Hooks []struct {
			Type           string            `json:"type"`
			URL            string            `json:"url"`
			Headers        map[string]string `json:"headers"`
			AllowedEnvVars []string          `json:"allowedEnvVars"`
			Timeout        int               `json:"timeout"`
		} `json:"hooks"`
	} `json:"hooks"`
}

func parseSettings(t *testing.T, raw string) claudeSettings {
	t.Helper()
	var doc claudeSettings
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("invalid settings json: %v\n%s", err, raw)
	}
	return doc
}

func TestClaudeSettingsRegistersEveryTrackedEvent(t *testing.T) {
	url := "http://127.0.0.1:1234/hooks/SES-01"
	doc := parseSettings(t, ClaudeSettingsJSON(url))

	for _, event := range []string{
		"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse",
		"PermissionRequest", "Notification", "Stop", "SessionEnd",
	} {
		entries, ok := doc.Hooks[event]
		if !ok || len(entries) == 0 || len(entries[0].Hooks) == 0 {
			t.Fatalf("no hook registered for %s", event)
		}
		h := entries[0].Hooks[0]
		if h.Type != "http" || h.URL != url {
			t.Fatalf("%s hook = %+v, want http hook at %s", event, h, url)
		}
	}
}

// The launch id is passed by reference to the pane environment, so a relaunch
// never has to rewrite the settings, and the id stays off the command line.
func TestClaudeSettingsCarriesLaunchHeader(t *testing.T) {
	doc := parseSettings(t, ClaudeSettingsJSON("http://127.0.0.1:1234/hooks/SES-01"))

	for event, entries := range doc.Hooks {
		h := entries[0].Hooks[0]
		if got := h.Headers["X-Agentboard-Launch"]; got != "$AGENTBOARD_LAUNCH_ID" {
			t.Errorf("%s launch header = %q", event, got)
		}
		// Claude Code interpolates a header variable only when it is listed.
		if len(h.AllowedEnvVars) != 1 || h.AllowedEnvVars[0] != "AGENTBOARD_LAUNCH_ID" {
			t.Errorf("%s allowedEnvVars = %v", event, h.AllowedEnvVars)
		}
	}
}

// Claude Code blocks on every hook. A lifecycle report must give up quickly if
// the board is gone; a permission request is a human decision and may wait.
func TestClaudeSettingsTimeouts(t *testing.T) {
	doc := parseSettings(t, ClaudeSettingsJSON("http://127.0.0.1:1234/hooks/SES-01"))

	if got := doc.Hooks["Stop"][0].Hooks[0].Timeout; got != 5 {
		t.Errorf("Stop timeout = %d, want 5", got)
	}
	if got := doc.Hooks["PermissionRequest"][0].Hooks[0].Timeout; got < 60 {
		t.Errorf("PermissionRequest timeout = %d, want a human-scale wait", got)
	}
}
