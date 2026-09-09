package agents

import (
	"regexp"

	"github.com/ayan-de/agent-board/internal/activity"
)

// claudeCode takes its hooks per process through `claude --settings`, so
// nothing is written into the ticket worktree and no earlier launch's settings
// can linger. It reads a permission decision nested under a behavior key.
var claudeCode = Descriptor{
	Name:         "claude-code",
	Binary:       "claude",
	Spawnable:    true,
	Capabilities: []Capability{CapActivityReports, CapPermissionRemote},
	Decisions:    activity.NestedDecision,
	Provision: func(p Provision) (Launch, error) {
		return Launch{Settings: activity.ClaudeSettingsJSON(p.HookURL())}, nil
	},
	Resume: regexp.MustCompile(`(?i)(claude\s+--resume\s+\S+)`),
	Logo:   " ▐▛███▜▌\n▝▜█████▛▘\n  ▘▘ ▝▝",
	Color:  "#D97757",
}
