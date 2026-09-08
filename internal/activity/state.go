// Package activity tracks what each spawned agent is doing, from hook reports
// the agents POST back to a loopback receiver.
package activity

import "encoding/json"

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

// payload is the Claude Code hook shape. FreeCode emits the same fields, so
// there is no per-agent mapper.
type payload struct {
	HookEventName    string          `json:"hook_event_name"`
	NotificationType string          `json:"notification_type"`
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
		case "agent_needs_input":
			return StateWaitingInput, true
		case "idle_prompt":
			return StateIdle, true
		}
	}
	return "", false
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
