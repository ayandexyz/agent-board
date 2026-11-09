package activity

import "encoding/json"

// pending is one PermissionRequest whose HTTP response is being held open
// while a human decides. One per session: an agent blocks on its dialog and
// cannot raise a second one until this is answered.
type pending struct {
	decision chan string
}

// DecisionCodec renders a permission decision in the reply shape its agent
// reads. The shape travels with the launch rather than being switched on an
// agent name in here, so an agent that reads a different shape needs only a
// codec of its own.
type DecisionCodec func(decision string) any

// NestedDecision is Claude Code's shape, which FreeCode reads too:
// {"decision":{"behavior":"allow"}}.
func NestedDecision(decision string) any {
	return claudeDecision{Behavior: decision}
}

// Resolve answers a session's held permission request. decision is "allow",
// "deny", or "" to hand the choice back to the agent's own pane picker. It
// reports whether a request was actually waiting.
func (r *Receiver) Resolve(sessionID, decision string) bool {
	r.mu.Lock()
	p, ok := r.pending[sessionID]
	r.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case p.decision <- decision:
		return true
	default:
		return false
	}
}

func (r *Receiver) park(sessionID string) *pending {
	p := &pending{decision: make(chan string, 1)}
	r.mu.Lock()
	r.pending[sessionID] = p
	r.mu.Unlock()
	return p
}

func (r *Receiver) unpark(sessionID string, p *pending) {
	r.mu.Lock()
	if r.pending[sessionID] == p {
		delete(r.pending, sessionID)
	}
	r.mu.Unlock()
}

type claudeDecision struct {
	Behavior string `json:"behavior"`
}

// permissionReply renders the decision through the launching agent's codec. An
// empty decision, or an agent that declared no codec, is no decision at all:
// the agent falls through to its own picker in the pane.
func permissionReply(codec DecisionCodec, decision string) []byte {
	if decision == "" || codec == nil {
		return []byte("{}")
	}
	raw, err := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PermissionRequest",
		"decision":      codec(decision),
	}})
	if err != nil {
		return []byte("{}")
	}
	return raw
}
