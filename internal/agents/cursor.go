package agents

// cursor is reached over MCP rather than spawned into a pane, so it reports no
// activity and prints no resume line.
var cursor = Descriptor{
	Name:      "cursor",
	Binary:    "cursor",
	Spawnable: true,
	Logo:      " ▛▀▀▀▜\n ▌ ▗▖ ▐\n ▙▄▄▄▟",
	Color:     "#F0DB4F",
}
