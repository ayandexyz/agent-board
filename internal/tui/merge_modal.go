package tui

import (
	"fmt"

	"github.com/ayan-de/agent-board/internal/orchestrator"
	"github.com/ayan-de/agent-board/internal/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type mergeTicketRequestMsg struct {
	ticketID string
}

type mergeConfirmMsg struct {
	ticketID string
	strategy orchestrator.MergeStrategy
}

type mergeResultMsg struct {
	ticketID string
	result   orchestrator.MergeResult
	err      error
}

type MergeModal struct {
	active   bool
	ticketID string
	branch   string
	strategy orchestrator.MergeStrategy
	cursor   int
	width    int
	height   int
	styles   ConfirmModalStyles
}

func (m *MergeModal) SetTheme(t *theme.Theme) {
	m.styles = NewConfirmModalStyles(t)
}

func (m *MergeModal) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *MergeModal) Active() bool {
	return m.active
}

func (m *MergeModal) Open(ticketID, branch string, strategy orchestrator.MergeStrategy) {
	m.active = true
	m.ticketID = ticketID
	m.branch = branch
	if strategy == "" {
		strategy = orchestrator.MergeStrategyLocal
	}
	m.strategy = strategy
	m.cursor = 1
}

func (m *MergeModal) Close() {
	m.active = false
	m.ticketID = ""
	m.branch = ""
}

func (m *MergeModal) toggleStrategy() {
	if m.strategy == orchestrator.MergeStrategyLocal {
		m.strategy = orchestrator.MergeStrategyPR
	} else {
		m.strategy = orchestrator.MergeStrategyLocal
	}
}

func (m MergeModal) Update(msg tea.Msg) (MergeModal, tea.Cmd) {
	if !m.active {
		return m, nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "left", "h":
		if m.cursor > 0 {
			m.cursor--
		}
	case "right", "l":
		if m.cursor < 1 {
			m.cursor++
		}
	case "s":
		m.toggleStrategy()
	case "esc":
		m.Close()
	case "enter":
		if m.cursor == 0 {
			ticketID := m.ticketID
			strategy := m.strategy
			m.Close()
			return m, func() tea.Msg {
				return mergeConfirmMsg{ticketID: ticketID, strategy: strategy}
			}
		}
		m.Close()
	}

	return m, nil
}

func (m MergeModal) strategyLabel() string {
	if m.strategy == orchestrator.MergeStrategyPR {
		return "pr (push + gh pr create)"
	}
	return "local (rebase + ff-only merge)"
}

func (m MergeModal) View() string {
	if !m.active {
		return ""
	}

	h := m.height - 6
	if h < 10 {
		h = 10
	}
	return lipgloss.Place(m.width, h,
		lipgloss.Center, lipgloss.Center,
		m.ViewBox(),
	)
}

func (m MergeModal) ViewBox() string {
	branch := m.branch
	if branch == "" {
		branch = "(branch created at start)"
	}

	message := fmt.Sprintf("Branch: %s\nStrategy: %s\n\nPress s to switch strategy.", branch, m.strategyLabel())

	merge := "[ Merge ]"
	cancel := "[ Cancel ]"
	if m.cursor == 0 {
		merge = m.styles.Highlight.Render(merge)
		cancel = m.styles.Cancel.Render(cancel)
	} else {
		merge = m.styles.Confirm.Render(merge)
		cancel = m.styles.Highlight.Render(cancel)
	}

	boxWidth := 48
	messageStyle := m.styles.Message.Width(boxWidth - 4)
	content := m.styles.Title.Render("Merge "+m.ticketID) + "\n\n" +
		messageStyle.Render(message) + "\n\n" +
		fmt.Sprintf("  %s    %s", merge, cancel)

	return m.styles.Border.Width(boxWidth).Render(content)
}
