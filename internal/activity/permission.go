package activity

import "encoding/json"

// pending is one PermissionRequest whose HTTP response is being held open
// while a human decides. One per session: an agent blocks on its dialog and
// cannot raise a second one until this is answered.
type pending struct {
	decision chan string
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

// permissionReply renders the decision in the shape the answering agent reads.
// Claude Code wants it nested under a behavior key; FreeCode wants a bare
// string. An empty decision is no decision at all: the agent falls through to
// its own picker in the pane.
func permissionReply(decision string, bare bool) []byte {
	if decision == "" {
		return []byte("{}")
	}
	out := map[string]any{"hookEventName": "PermissionRequest"}
	if bare {
		out["decision"] = decision
	} else {
		out["decision"] = claudeDecision{Behavior: decision}
	}
	raw, err := json.Marshal(map[string]any{"hookSpecificOutput": out})
	if err != nil {
		return []byte("{}")
	}
	return raw
}
