package tui

import (
	"fmt"
	"strings"

	"github.com/ayan-de/agent-board/internal/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// WaitingItem is one session that needs a human, as the overlay shows it.
type WaitingItem struct {
	SessionID string
	TicketID  string
	State     string
	ToolName  string
	Detail    string
	// Decidable means a hook is holding the request open and y/n answer it.
	// Otherwise the only thing to do is jump to the pane.
	Decidable bool
}

type waitingResolveMsg struct {
	sessionID string
	decision  string
}

type waitingJumpMsg struct {
	sessionID string
}

type waitingClosedMsg struct{}

// WaitingOverlay lists every agent waiting on the user and lets them answer a
// permission request or jump to the pane without leaving the board.
type WaitingOverlay struct {
	active bool
	items  []WaitingItem
	cursor int
	width  int
	height int
	styles ConfirmModalStyles
}

func (o *WaitingOverlay) SetTheme(t *theme.Theme) {
	o.styles = NewConfirmModalStyles(t)
}

func (o *WaitingOverlay) SetSize(w, h int) {
	o.width = w
	o.height = h
}

func (o *WaitingOverlay) Active() bool {
	return o.active
}

// Open shows the given items. Reopening keeps the cursor on the same row
// where possible, so answering one request does not jump the selection.
func (o *WaitingOverlay) Open(items []WaitingItem) {
	o.active = true
	o.items = items
	if o.cursor >= len(items) {
		o.cursor = len(items) - 1
	}
	if o.cursor < 0 {
		o.cursor = 0
	}
}

func (o *WaitingOverlay) Close() {
	o.active = false
	o.items = nil
	o.cursor = 0
}

func (o *WaitingOverlay) selected() (WaitingItem, bool) {
	if o.cursor < 0 || o.cursor >= len(o.items) {
		return WaitingItem{}, false
	}
	return o.items[o.cursor], true
}

func (o WaitingOverlay) Update(msg tea.Msg) (WaitingOverlay, tea.Cmd) {
	if !o.active {
		return o, nil
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return o, nil
	}

	switch keyMsg.String() {
	case "j", "down":
		if o.cursor < len(o.items)-1 {
			o.cursor++
		}
	case "k", "up":
		if o.cursor > 0 {
			o.cursor--
		}
	case "y", "n":
		it, ok := o.selected()
		if !ok || !it.Decidable {
			return o, nil
		}
		decision := "allow"
		if keyMsg.String() == "n" {
			decision = "deny"
		}
		sessionID := it.SessionID
		return o, func() tea.Msg {
			return waitingResolveMsg{sessionID: sessionID, decision: decision}
		}
	case "enter":
		it, ok := o.selected()
		if !ok {
			return o, nil
		}
		sessionID := it.SessionID
		o.Close()
		return o, func() tea.Msg {
			return waitingJumpMsg{sessionID: sessionID}
		}
	case "esc", "q", "W":
		o.Close()
		return o, func() tea.Msg { return waitingClosedMsg{} }
	}
	return o, nil
}

func (o WaitingOverlay) ViewBox() string {
	boxWidth := 64
	if o.width > 0 && boxWidth > o.width-4 {
		boxWidth = o.width - 4
	}
	if boxWidth < 30 {
		boxWidth = 30
	}
	inner := boxWidth - 4

	title := o.styles.Title.Render(fmt.Sprintf("%d waiting", len(o.items)))

	var rows []string
	if len(o.items) == 0 {
		rows = append(rows, o.styles.Message.Render("No agent is waiting on you."))
	}
	for i, it := range o.items {
		rows = append(rows, o.renderRow(it, i == o.cursor, inner))
	}

	footer := o.styles.Cancel.Render("y allow  n deny  enter jump  esc close")

	content := title + "\n\n" + strings.Join(rows, "\n") + "\n\n" + footer
	return o.styles.Border.Width(boxWidth).Render(content)
}

func (o WaitingOverlay) renderRow(it WaitingItem, selected bool, width int) string {
	name := it.TicketID
	if name == "" {
		name = it.SessionID
	}
	glyph := ActivityGlyph(it.State)
	if glyph == "" {
		glyph = " "
	}
	what := it.Detail
	if what == "" {
		what = it.ToolName
	}
	if what == "" {
		what = "waiting for your answer"
	}
	if it.Decidable {
		what = "approve? " + what
	}
	line := fmt.Sprintf("%s %s  %s", glyph, name, what)
	line = truncateRunes(line, width-2)

	style := o.styles.Message
	if selected {
		style = o.styles.Highlight
		line = "> " + line
	} else {
		line = "  " + line
	}
	return style.Render(line)
}
