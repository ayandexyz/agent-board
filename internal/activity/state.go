// Package activity tracks what each spawned agent is doing, from hook reports
// the agents POST back to a loopback receiver.
package activity

import (
	"encoding/json"
	"strings"
)

type State string

const (
	StateActive       State = "active"
	StateIdle         State = "idle"
	StateWaitingInput State = "waiting_input"
	StateBlocked      State = "blocked"
	StateExited       State = "exited"
)

// NeedsInput reports whether the session cannot make progress without a human.
func (s State) NeedsInput() bool {
	return s == StateWaitingInput || s == StateBlocked
}

// payload is the Claude Code hook shape. FreeCode's hooks emit the same
// fields, so there is no per-agent mapper.
type payload struct {
	HookEventName    string          `json:"hook_event_name"`
	NotificationType string          `json:"notification_type"`
	Message          string          `json:"message"`
	ToolName         string          `json:"tool_name"`
	ToolInput        json.RawMessage `json:"tool_input"`
	Reason           string          `json:"reason"`
}

// next returns the state a payload implies, and whether it implies one at all.
func (p payload) next() (State, bool) {
	switch p.HookEventName {
	case "SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse":
		return StateActive, true
	case "PermissionRequest":
		// A question reaches the board as a permission request for the tool
		// that asks it, but what is waiting is an answer, not an approval.
		if p.ToolName == "AskUserQuestion" {
			return StateWaitingInput, true
		}
		return StateBlocked, true
	case "Stop":
		return StateIdle, true
	case "SessionEnd":
		if p.Reason == "clear" || p.Reason == "resume" {
			return StateIdle, true
		}
		return StateExited, true
	case "Notification":
		switch p.NotificationType {
		case "permission_prompt":
			return StateBlocked, true
		case "agent_needs_input", "question":
			return StateWaitingInput, true
		case "idle_prompt":
			return StateIdle, true
		}
	}
	return "", false
}

// detail is the line an alert shows: the tool call when the payload carries
// one, otherwise the notification's own message.
func (p payload) detail(toolName string) string {
	if len(p.ToolInput) == 0 && strings.TrimSpace(p.Message) != "" {
		return truncate(collapse(p.Message), detailLimit)
	}
	return toolDetail(toolName, p.ToolInput)
}

// asksQuestion reports whether a permission request is really a question.
// Claude Code delivers AskUserQuestion as a PermissionRequest, but allowing it
// only lets the agent ask — the answer is typed in the pane either way. The
// board must not hold the hook open for a decision nobody can make, nor offer
// an approve/deny modal that would not answer anything.
func (p payload) asksQuestion() bool {
	return p.HookEventName == "PermissionRequest" && p.ToolName == "AskUserQuestion"
}

// clearsBlock reports whether an event may move a blocked session. Parallel
// subagent tool traffic must not clear a permission dialog that is still open.
//
// ponytail: no tool_use_id matching yet — any PostToolUse clears the block, not
// only the one that raised it. Add the tool-flight map when parallel subagents
// start producing false clears.
func (p payload) clearsBlock() bool {
	switch p.HookEventName {
	case "UserPromptSubmit", "Stop", "SessionEnd", "PostToolUse":
		return true
	}
	return p.HookEventName == "Notification" && p.NotificationType != "permission_prompt"
}
