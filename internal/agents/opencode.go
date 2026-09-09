package agents

import (
	"regexp"

	"github.com/ayan-de/agent-board/internal/activity"
)

// openCode has no settings flag, so the board ships its reporter as a plugin
// loaded through a config document named by OPENCODE_CONFIG. OpenCode merges
// plugin lists across config sources, so the user's own plugins survive.
var openCode = Descriptor{
	Name:         "opencode",
	Binary:       "opencode",
	Spawnable:    true,
	Capabilities: []Capability{CapActivityReports, CapPermissionRemote},
	// The plugin reads either decision shape; the nested one keeps every
	// hook-shaped agent on one wire format.
	Decisions: activity.NestedDecision,
	Provision: func(p Provision) (Launch, error) {
		path, err := activity.OpenCodeConfig(p.CacheDir)
		if err != nil {
			return Launch{}, err
		}
		return Launch{Env: []string{"OPENCODE_CONFIG=" + path}}, nil
	},
	Resume: regexp.MustCompile(`(?i)(opencode\s+-s\s+\S+)`),
	Logo:   "  █▀▀█\n  █__█\n  ▀▀▀▀",
	Color:  "#808080",
}
