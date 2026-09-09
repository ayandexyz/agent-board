package agents

import "regexp"

// gemini is recognised but not offered: the board can pick its resume command
// out of a pane it did not start, and nothing more.
var gemini = Descriptor{
	Name:   "gemini",
	Binary: "gemini",
	Resume: regexp.MustCompile(`(?i)(gemini\s+--resume\s+\S+)`),
}
