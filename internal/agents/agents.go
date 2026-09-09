// Package agents is the board's catalog of coding agents: one file per agent,
// each declaring what the board may ask of it and how to wire it up at spawn.
//
// Everything the rest of the board switched on by agent name — the launch
// environment, the permission reply shape, the resume command, the picker's
// logo — is declared here once, so adding an agent is adding a file and
// removing a capability is deleting a constant.
package agents

import (
	"fmt"
	"regexp"

	"github.com/ayan-de/agent-board/internal/activity"
)

// Capability is one thing the board may ask of an agent. Only capabilities the
// board actually branches on belong here: a flag nothing reads is documentation
// pretending to be a type.
type Capability string

const (
	// CapActivityReports: the agent posts hook reports to the board's receiver,
	// so its state comes from the agent rather than from watching its pane.
	CapActivityReports Capability = "activity.reports"
	// CapPermissionRemote: a permission request can be answered over the wire,
	// so the board raises an approval modal instead of sending the user away.
	CapPermissionRemote Capability = "permission.remote"

// New capabilities are added here when something in the board branches on
// them — a flag no caller reads is documentation pretending to be a type.
// Candidates already visible in the code: lifecycle.session_end (OpenCode
// reports no exit, so only the pane monitor notices) and tool.flight_ids (no
// tool_use_id means any PostToolUse clears a permission block, not just the
// one that raised it).
)

// Provision is what an agent is told about the spawn it is being wired into.
type Provision struct {
	SessionID string
	LaunchID  string
	// HookBase is the receiver's base URL, e.g. http://127.0.0.1:41234. It is
	// never empty: an agent is only provisioned when a receiver is running.
	HookBase string
	// CacheDir is where an agent may write settings or plugin files. Never the
	// ticket worktree — nothing the board generates belongs in the user's repo.
	CacheDir string
}

// HookURL is the per-session endpoint an agent reports to.
func (p Provision) HookURL() string { return p.HookBase + "/hooks/" + p.SessionID }

// Launch is the agent-specific half of a spawn. The identity variables every
// agent gets are added by the orchestrator, not here.
type Launch struct {
	// Env is extra KEY=VALUE pairs to export into the agent's pane.
	Env []string
	// Settings is a settings document the runner caches to a file and passes to
	// the agent by path.
	Settings string
}

// Descriptor is one agent's contract with the board.
//
// It is a struct rather than an interface on purpose: an agent is overwhelmingly
// declaration, and most agents need no provisioning code at all. A literal in an
// init() reads as the contract it is; eight methods per agent would not.
type Descriptor struct {
	// Name is the board's identifier for the agent, as stored on a ticket.
	Name string
	// Binary is what is looked up on $PATH and typed into the pane.
	Binary string
	// Spawnable is false for an agent the board can recognise but does not
	// offer — it stays out of the picker while its resume pattern still works.
	Spawnable bool
	// Capabilities is everything the board may ask of this agent. Anything not
	// listed here degrades deliberately rather than by timing out.
	Capabilities []Capability
	// Decisions renders a permission decision in this agent's reply shape.
	// Required by, and only meaningful with, CapPermissionRemote.
	Decisions activity.DecisionCodec
	// Provision wires one spawn up to the board's receiver. Nil for an agent
	// that needs nothing beyond the identity variables — or nothing at all.
	Provision func(Provision) (Launch, error)
	// Resume recovers the command that resumes a finished session from the
	// agent's own farewell line in the pane. Nil if it prints none.
	Resume *regexp.Regexp
	// Logo and Color are how the agent is drawn in the picker.
	Logo  string
	Color string
}

// Supports reports whether the board may ask this of the agent.
func (d Descriptor) Supports(c Capability) bool {
	for _, have := range d.Capabilities {
		if have == c {
			return true
		}
	}
	return false
}

var (
	registry = map[string]Descriptor{}
	// order preserves registration order so the picker does not reshuffle
	// itself because a map ranged differently.
	order []string
)

// Register adds an agent to the catalog. Called from each agent file's init;
// it panics on a contradictory descriptor, which is a programming error caught
// at startup rather than at the moment a user is waiting on a modal.
func Register(d Descriptor) {
	switch {
	case d.Name == "":
		panic("agents.Register: descriptor has no name")
	case d.Binary == "":
		panic(fmt.Sprintf("agents.Register: %s has no binary", d.Name))
	case d.Supports(CapPermissionRemote) && d.Decisions == nil:
		panic(fmt.Sprintf("agents.Register: %s claims %s but supplies no decision codec", d.Name, CapPermissionRemote))
	case !d.Supports(CapPermissionRemote) && d.Decisions != nil:
		panic(fmt.Sprintf("agents.Register: %s supplies a decision codec without claiming %s", d.Name, CapPermissionRemote))
	case !d.Supports(CapActivityReports) && d.Provision != nil:
		panic(fmt.Sprintf("agents.Register: %s provisions reporting without claiming %s", d.Name, CapActivityReports))
	}
	if _, dup := registry[d.Name]; dup {
		panic(fmt.Sprintf("agents.Register: %s registered twice", d.Name))
	}
	registry[d.Name] = d
	order = append(order, d.Name)
}

// Lookup finds an agent by the name a ticket stores. Aliases are resolved
// first, so a ticket written before a rename still spawns.
func Lookup(name string) (Descriptor, bool) {
	if canonical, ok := aliases[name]; ok {
		name = canonical
	}
	d, ok := registry[name]
	return d, ok
}

// Supports is Lookup plus Supports, for the common "may I?" question about an
// agent the board only knows by name. An unknown agent supports nothing.
func Supports(name string, c Capability) bool {
	d, ok := Lookup(name)
	return ok && d.Supports(c)
}

// All lists the catalog in registration order.
func All() []Descriptor {
	out := make([]Descriptor, 0, len(order))
	for _, name := range order {
		out = append(out, registry[name])
	}
	return out
}

// Spawnable lists the agents the board offers in its picker.
func Spawnable() []Descriptor {
	var out []Descriptor
	for _, d := range All() {
		if d.Spawnable {
			out = append(out, d)
		}
	}
	return out
}

// ResumePatterns is every pattern that recovers a resume command, for scanning
// pane output whose agent is not known with certainty.
func ResumePatterns() []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, d := range All() {
		if d.Resume != nil {
			out = append(out, d.Resume)
		}
	}
	return out
}
