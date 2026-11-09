package agents

import (
	"regexp"

	"github.com/ayandexyz/agent-board/internal/activity"
)

// freeCode reads Claude Code's settings format, so it is wired up exactly like
// claudeCode: one http hook per event in a document passed with
// `freecode --settings`, and a permission decision nested under a behavior
// key. FreeCode itself knows nothing about the board.
var freeCode = Descriptor{
	Name:         "freecode",
	Binary:       "freecode",
	Spawnable:    true,
	Capabilities: []Capability{CapActivityReports, CapPermissionRemote},
	Decisions:    activity.NestedDecision,
	Provision: func(p Provision) (Launch, error) {
		return Launch{Settings: activity.ClaudeSettingsJSON(p.HookURL())}, nil
	},
	Resume: regexp.MustCompile(`(?i)(freecode\s+--resume\s+\S+)`),
	Logo:   "   █▀▀ \n   █▀▀ \n   ▀   ",
	Color:  "#e7c98a",
}
