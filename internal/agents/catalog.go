package agents

// catalog is the one place that says which agents exist. Each descriptor lives
// in its own file; this list only fixes the order they are offered in, which
// map iteration would otherwise scramble between runs.
func init() {
	for _, d := range []Descriptor{
		claudeCode,
		openCode,
		codex,
		cursor,
		freeCode,
		gemini,
	} {
		Register(d)
	}
}

// aliases maps names the board may still be storing on a ticket onto the
// canonical descriptor name, so a rename never strands an existing ticket.
var aliases = map[string]string{
	"claudecode": "claude-code",
}
