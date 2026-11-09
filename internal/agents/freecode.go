package agents

import (
	"regexp"

	"github.com/ayandexyz/agent-board/internal/activity"
)

// freeCode posts Claude-Code-shaped payloads from its own built-in board
// webhook, so it needs no document from the board — only the endpoint. It
// reads a permission decision as a bare string.
var freeCode = Descriptor{
	Name:         "freecode",
	Binary:       "freecode",
	Spawnable:    true,
	Capabilities: []Capability{CapActivityReports, CapPermissionRemote},
	Decisions:    activity.BareDecision,
	Provision: func(p Provision) (Launch, error) {
		return Launch{Env: []string{"FREECODE_HOOK_URL=" + p.HookURL()}}, nil
	},
	Resume: regexp.MustCompile(`(?i)(freecode\s+--resume\s+\S+)`),
	Logo:   "   █▀▀ \n   █▀▀ \n   ▀   ",
	Color:  "#e7c98a",
}
