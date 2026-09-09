package agents

import "regexp"

// codex is spawnable but reports nothing: its state comes from watching the
// pane. Give it CapActivityReports and a Provision when a reporter exists.
var codex = Descriptor{
	Name:      "codex",
	Binary:    "codex",
	Spawnable: true,
	Resume:    regexp.MustCompile(`(?i)(codex\s+--resume\s+\S+)`),
	Logo:      " ▗▄▄▄▖\n ▐▝▞▙▌\n ▝▀▀▀▘",
	Color:     "#10A37F",
}
