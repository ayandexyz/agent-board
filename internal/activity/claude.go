package activity

import (
	"encoding/json"
	"strings"
)

// Events Claude Code reports to the board. PermissionRequest is the only one
// the agent will wait on for an answer; the rest are lifecycle reports.
var claudeHookEvents = []string{
	"SessionStart",
	"UserPromptSubmit",
	"PreToolUse",
	"PostToolUse",
	"PermissionRequest",
	"Notification",
	"Stop",
	"SessionEnd",
}

const (
	// Claude Code runs hooks inline, so a lifecycle report must not outlive the
	// user's patience if the board went away.
	claudeReportTimeout = 5
	// A permission request waits on a human. Claude Code's own default for an
	// http hook is ten minutes; match it explicitly rather than inherit it.
	claudePermissionTimeout = 600
)

type claudeHookHandler struct {
	Type           string            `json:"type"`
	URL            string            `json:"url"`
	Headers        map[string]string `json:"headers"`
	AllowedEnvVars []string          `json:"allowedEnvVars"`
	Timeout        int               `json:"timeout"`
}

type claudeHookMatcher struct {
	Hooks []claudeHookHandler `json:"hooks"`
}

// ClaudeSettingsJSON is the inline document for `claude --settings`, pointing
// every tracked hook event at hookURL. It is passed per process, so nothing is
// written into the ticket's worktree.
//
// The launch id is sent as a header reference rather than a literal: Claude
// Code interpolates $AGENTBOARD_LAUNCH_ID from the pane environment, but only
// because allowedEnvVars names it.
func ClaudeSettingsJSON(hookURL string) string {
	hooks := make(map[string][]claudeHookMatcher, len(claudeHookEvents))
	for _, event := range claudeHookEvents {
		timeout := claudeReportTimeout
		if event == "PermissionRequest" {
			timeout = claudePermissionTimeout
		}
		hooks[event] = []claudeHookMatcher{{Hooks: []claudeHookHandler{{
			Type:           "http",
			URL:            hookURL,
			Headers:        map[string]string{"X-Agentboard-Launch": "$AGENTBOARD_LAUNCH_ID"},
			AllowedEnvVars: []string{"AGENTBOARD_LAUNCH_ID"},
			Timeout:        timeout,
		}}}}
	}
	raw, err := json.Marshal(struct {
		Hooks map[string][]claudeHookMatcher `json:"hooks"`
	}{Hooks: hooks})
	if err != nil {
		return ""
	}
	// The document is typed into the pane as one single-quoted shell word.
	return strings.ReplaceAll(string(raw), "'", "")
}
