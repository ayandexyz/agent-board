package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ayan-de/agent-board/internal/config"
	"github.com/ayan-de/agent-board/internal/keybinding"
	"github.com/ayan-de/agent-board/internal/orchestrator"
	"github.com/ayan-de/agent-board/internal/store"
	"github.com/ayan-de/agent-board/internal/theme"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ticketViewModeType int

const (
	ticketViewMode ticketViewModeType = iota
	ticketEditMode
	ticketAgentSelectMode
	ticketPrioritySelectMode
	ticketDependsOnSelectMode
	ticketAdHocPromptMode
	ticketBranchModeSelectMode
)

var branchModeOptions = [2]string{store.BranchModeCurrent, store.BranchModeWorktree}

func branchModeLabel(mode string) string {
	if mode == store.BranchModeCurrent {
		return "Current branch"
	}
	return "Worktree branch"
}

var statusCycle = [4]string{"backlog", "in_progress", "review", "done"}

type ticketField struct {
	label    string
	value    func(t *store.Ticket) string
	editable bool
	set      func(t *store.Ticket, v string)
}

type TicketViewStyles struct {
	Border       lipgloss.Style
	Title        lipgloss.Style
	Label        lipgloss.Style
	LabelActive  lipgloss.Style
	Value        lipgloss.Style
	SelectedRow  lipgloss.Style
	Cursor       lipgloss.Style
	Caret        lipgloss.Style
	CaretCell    lipgloss.Style
	EditBox      lipgloss.Style
	Footer       lipgloss.Style
	Empty        lipgloss.Style
	Rule         lipgloss.Style
	Badge        lipgloss.Style
	Panel        lipgloss.Style
	PanelTitle   lipgloss.Style
	OptionActive lipgloss.Style

	OnAccent     lipgloss.Color
	MutedColor   lipgloss.Color
	AccentColor  lipgloss.Color
	SuccessColor lipgloss.Color
	WarningColor lipgloss.Color

	StatusColors   map[string]lipgloss.Color
	PriorityColors map[string]lipgloss.Color
}

type TicketViewModel struct {
	board    Board
	resolver *keybinding.Resolver
	width    int
	height   int

	ticket      *store.Ticket
	fields      []ticketField
	cursor      int
	mode        ticketViewModeType
	editBuffer  string
	editCursor  int
	agents      []config.DetectedAgent
	agentCursor int

	priorities     []string
	priorityCursor int

	branchPrefix     string
	currentBranch    string
	branchModeCursor int

	dependsOnTickets  []store.Ticket
	dependsOnCursor   int
	dependsOnSelected []string

	styles TicketViewStyles

	activeProposal *store.Proposal
	loading        bool

	adhocAgent        string
	adhocPrompt       string
	adhocPromptCursor int

	viewport viewport.Model
}

func DefaultTicketViewStyles() TicketViewStyles {
	return TicketViewStyles{
		Border: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("69")).
			Padding(1, 2),
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("69")),
		Label: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("252")),
		Value: lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")),
		SelectedRow: lipgloss.NewStyle().
			Background(lipgloss.Color("69")).
			Foreground(lipgloss.Color("15")),
		Cursor: lipgloss.NewStyle().
			Foreground(lipgloss.Color("69")),
		Caret: lipgloss.NewStyle().
			Foreground(lipgloss.Color("213")),
		CaretCell: lipgloss.NewStyle().
			Foreground(lipgloss.Color("235")).
			Background(lipgloss.Color("213")),
		EditBox: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("213")),
		Footer: lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")),
		Empty: lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")),
		LabelActive: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("235")).
			Background(lipgloss.Color("69")).
			Padding(0, 1),
		Rule: lipgloss.NewStyle().
			Foreground(lipgloss.Color("238")),
		Badge: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("235")).
			Background(lipgloss.Color("69")).
			Padding(0, 1),
		Panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("213")).
			Padding(0, 1),
		PanelTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("213")),
		OptionActive: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("69")),
		OnAccent:     lipgloss.Color("235"),
		MutedColor:   lipgloss.Color("240"),
		AccentColor:  lipgloss.Color("213"),
		SuccessColor: lipgloss.Color("42"),
		WarningColor: lipgloss.Color("208"),
		StatusColors: map[string]lipgloss.Color{
			"backlog":     lipgloss.Color("240"),
			"in_progress": lipgloss.Color("208"),
			"review":      lipgloss.Color("213"),
			"done":        lipgloss.Color("42"),
		},
		PriorityColors: map[string]lipgloss.Color{
			"low":      lipgloss.Color("240"),
			"medium":   lipgloss.Color("69"),
			"high":     lipgloss.Color("208"),
			"critical": lipgloss.Color("196"),
		},
	}
}

func NewTicketViewStyles(t *theme.Theme) TicketViewStyles {
	return TicketViewStyles{
		Border: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.Primary).
			Padding(1, 2),
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Primary),
		Label: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Text),
		Value: lipgloss.NewStyle().
			Foreground(t.Text),
		SelectedRow: lipgloss.NewStyle().
			Background(t.Primary).
			Foreground(t.Text),
		Cursor: lipgloss.NewStyle().
			Foreground(t.Primary),
		Caret: lipgloss.NewStyle().
			Foreground(t.Accent),
		CaretCell: lipgloss.NewStyle().
			Foreground(t.Background).
			Background(t.Accent),
		EditBox: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(t.Accent),
		Footer: lipgloss.NewStyle().
			Foreground(t.TextMuted),
		Empty: lipgloss.NewStyle().
			Foreground(t.TextMuted),
		LabelActive: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Background).
			Background(t.Primary).
			Padding(0, 1),
		Rule: lipgloss.NewStyle().
			Foreground(t.Border),
		Badge: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Background).
			Background(t.Primary).
			Padding(0, 1),
		Panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.Accent).
			Padding(0, 1),
		PanelTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Accent),
		OptionActive: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.Primary),
		OnAccent:     t.Background,
		MutedColor:   t.TextMuted,
		AccentColor:  t.Accent,
		SuccessColor: t.Success,
		WarningColor: t.Warning,
		StatusColors: map[string]lipgloss.Color{
			"backlog":     t.TextMuted,
			"in_progress": t.Warning,
			"review":      t.Accent,
			"done":        t.Success,
		},
		PriorityColors: map[string]lipgloss.Color{
			"low":      t.TextMuted,
			"medium":   t.Info,
			"high":     t.Warning,
			"critical": t.Error,
		},
	}
}

func ticketFields() []ticketField {
	return []ticketField{
		{
			label:    "ID",
			value:    func(t *store.Ticket) string { return t.ID },
			editable: false,
		},
		{
			label:    "Title",
			value:    func(t *store.Ticket) string { return t.Title },
			editable: true,
			set:      func(t *store.Ticket, v string) { t.Title = v },
		},
		{
			label:    "Prompt",
			value:    func(t *store.Ticket) string { return t.Prompt },
			editable: true,
			set:      func(t *store.Ticket, v string) { t.Prompt = v },
		},
		{
			label:    "Status",
			value:    func(t *store.Ticket) string { return t.Status },
			editable: false,
		},
		{
			label:    "Priority",
			value:    func(t *store.Ticket) string { return t.Priority },
			editable: false,
		},
		{
			label:    "Agent",
			value:    func(t *store.Ticket) string { return t.Agent },
			editable: false,
		},
		{
			label:    "Branch",
			value:    func(t *store.Ticket) string { return t.Branch },
			editable: true,
			set:      func(t *store.Ticket, v string) { t.Branch = v },
		},
		{
			label: "Tags",
			value: func(t *store.Ticket) string {
				if len(t.Tags) == 0 {
					return ""
				}
				return strings.Join(t.Tags, ", ")
			},
			editable: true,
			set: func(t *store.Ticket, v string) {
				t.Tags = parseTags(v)
			},
		},
		{
			label: "Depends On",
			value: func(t *store.Ticket) string {
				if len(t.DependsOn) == 0 {
					return ""
				}
				return strings.Join(t.DependsOn, ", ")
			},
			editable: false,
		},
		{
			label: "Created",
			value: func(t *store.Ticket) string {
				return t.CreatedAt.Format(time.DateTime)
			},
			editable: false,
		},
		{
			label: "Updated",
			value: func(t *store.Ticket) string {
				return t.UpdatedAt.Format(time.DateTime)
			},
			editable: false,
		},
		{
			label: "Resume",
			value: func(t *store.Ticket) string {
				if t.ResumeCommand == "" {
					return "—"
				}
				return t.ResumeCommand
			},
			editable: false,
		},
	}
}

func (m TicketViewModel) branchDisplay() string {
	if m.ticket == nil {
		return ""
	}
	mode := "worktree"
	branch := m.ticket.Branch
	if !m.ticket.UsesWorktree() {
		mode = "current"
		branch = m.currentBranch
	} else if branch == "" {
		branch = orchestrator.DefaultBranchName(m.branchPrefix, m.ticket.ID)
	}
	if branch == "" {
		return m.styles.Empty.Render(mode + " branch")
	}
	return m.styles.Empty.Render(mode+" · ") +
		lipgloss.NewStyle().Foreground(m.styles.AccentColor).Render(branch)
}

func (m TicketViewModel) helpFooter() string {
	km := m.resolver.KeyMap()
	return km.TicketViewHelp() + " │ Esc: back"
}

func NewTicketViewModel(board Board, resolver *keybinding.Resolver, t *theme.Theme, agents []config.DetectedAgent) TicketViewModel {
	return TicketViewModel{
		board:        board,
		resolver:     resolver,
		styles:       NewTicketViewStyles(t),
		fields:       ticketFields(),
		mode:         ticketViewMode,
		agents:       agents,
		priorities:   []string{"", "low", "medium", "high", "critical"},
		branchPrefix: "agent/",
		viewport:     viewport.New(0, 0),
	}
}

// saveTicket persists the ticket's editable fields through the board and
// takes back what it stored, so the pointer the app shares sees the same
// ticket the database does.
func (m TicketViewModel) saveTicket() {
	if m.ticket == nil {
		return
	}
	if updated, err := m.board.UpdateTicket(context.Background(), *m.ticket); err == nil {
		*m.ticket = updated
	}
}

func (m TicketViewModel) SetGitContext(currentBranch, branchPrefix string) TicketViewModel {
	m.currentBranch = currentBranch
	if branchPrefix != "" {
		m.branchPrefix = branchPrefix
	}
	return m
}

func (m TicketViewModel) Init() tea.Cmd {
	return nil
}

func (m TicketViewModel) Update(msg tea.Msg) (TicketViewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vh := m.height - 7
		if vh < 0 {
			vh = 0
		}
		m.viewport.Width = m.width - 6
		m.viewport.Height = vh
		return m, nil
	case tea.KeyMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		m, nextCmd := m.handleKey(msg)
		return m, tea.Batch(cmd, nextCmd)
	}
	return m, nil
}

func (m TicketViewModel) handleKey(msg tea.KeyMsg) (TicketViewModel, tea.Cmd) {
	if m.mode == ticketEditMode {
		return m.handleEditKey(msg)
	}
	if m.mode == ticketAgentSelectMode {
		return m.handleAgentSelectKey(msg)
	}
	if m.mode == ticketPrioritySelectMode {
		return m.handlePrioritySelectKey(msg)
	}
	if m.mode == ticketDependsOnSelectMode {
		return m.handleDependsOnSelectKey(msg)
	}
	if m.mode == ticketAdHocPromptMode {
		return m.handleAdHocPromptKey(msg)
	}
	if m.mode == ticketBranchModeSelectMode {
		return m.handleBranchModeSelectKey(msg)
	}
	return m.handleViewKey(msg)
}

func (m TicketViewModel) handleViewKey(msg tea.KeyMsg) (TicketViewModel, tea.Cmd) {
	key := msg.String()
	action, _ := m.resolver.Resolve(key)

	switch action {
	case keybinding.ActionNextTicket:
		if m.cursor < len(m.fields)-1 {
			m.cursor++
		}
	case keybinding.ActionPrevTicket:
		if m.cursor > 0 {
			m.cursor--
		}
	case keybinding.ActionInteract:
		if m.ticket != nil && m.cursor < len(m.fields) && m.fields[m.cursor].label == "Branch" {
			m.mode = ticketBranchModeSelectMode
			m.branchModeCursor = 1
			if !m.ticket.UsesWorktree() {
				m.branchModeCursor = 0
			}
			return m, nil
		}
		if m.ticket != nil && m.cursor < len(m.fields) && m.fields[m.cursor].editable {
			if m.fields[m.cursor].label == "Title" {
				m.editBuffer = ""
			} else {
				m.editBuffer = m.fields[m.cursor].value(m.ticket)
			}
			m.editCursor = len([]rune(m.editBuffer))
			m.mode = ticketEditMode
		}
	case keybinding.ActionAssignAgent:
		if m.ticket != nil {
			m.mode = ticketAgentSelectMode
			m.agentCursor = 0
		}
	case keybinding.ActionSetPriority:
		if m.ticket != nil {
			m.mode = ticketPrioritySelectMode
			m.priorityCursor = 0
			for i, p := range m.priorities {
				if p == m.ticket.Priority {
					m.priorityCursor = i
					break
				}
			}
		}
	case keybinding.ActionCycleStatus:
		if m.ticket != nil {
			return m.cycleStatus()
		}
	case keybinding.ActionMergeTicket:
		if m.ticket != nil {
			if m.ticket.Status != "review" {
				return m, func() tea.Msg {
					return notificationMsg{
						title:   "Cannot merge",
						message: "Only tickets in Review can be merged",
						variant: NotificationInfo,
					}
				}
			}
			ticketID := m.ticket.ID
			return m, func() tea.Msg {
				return mergeTicketRequestMsg{ticketID: ticketID}
			}
		}
	case keybinding.ActionGenerateProposal:
		if m.ticket != nil {
			ticketID := m.ticket.ID
			return m, func() tea.Msg {
				return generateProposalMsg{ticketID: ticketID}
			}
		}
	case keybinding.ActionApproveProposal:
		if m.activeProposal != nil && m.activeProposal.Status == "pending" {
			proposalID := m.activeProposal.ID
			return m, func() tea.Msg {
				return proposalApprovedMsg{proposalID: proposalID}
			}
		}
	case keybinding.ActionViewProposal:
		if m.activeProposal != nil {
			return m, func() tea.Msg {
				return viewProposalFullMsg{proposalID: m.activeProposal.ID}
			}
		}
	case keybinding.ActionSetDependsOn:
		if m.ticket != nil {
			m.mode = ticketDependsOnSelectMode
			m.dependsOnCursor = 0
			m.dependsOnSelected = m.ticket.DependsOn
			tickets, _ := m.board.ListTickets(context.Background(), store.TicketFilters{})
			m.dependsOnTickets = tickets
			if len(m.dependsOnTickets) > 5 {
				m.dependsOnTickets = m.dependsOnTickets[:5]
			}
		}
	case keybinding.ActionStartRun:
		if m.ticket != nil {
			if m.ticket.Status != "in_progress" {
				return m, func() tea.Msg {
					return notificationMsg{
						title:   "Cannot start run",
						message: "Move ticket to In Progress to start the agent working",
						variant: NotificationInfo,
					}
				}
			}
			if m.activeProposal != nil {
				if m.activeProposal.Status == "approved" {
					return m, func() tea.Msg {
						return runStartedMsg{proposalID: m.activeProposal.ID}
					}
				}
				// Proposal exists but not approved - show notification
				return m, func() tea.Msg {
					return notificationMsg{
						title:   "Cannot start run",
						message: "Approve proposal to start execution",
						variant: NotificationInfo,
					}
				}
			}
			// No proposal - direct run with ticket title + prompt
			if m.ticket.Agent == "" {
				return m, func() tea.Msg {
					return notificationMsg{
						title:   "Cannot start run",
						message: "Assign an agent to this ticket first",
						variant: NotificationInfo,
					}
				}
			}
			return m, func() tea.Msg {
				return directRunStartedMsg{ticketID: m.ticket.ID, agent: m.ticket.Agent, prompt: m.buildPromptFromTicket()}
			}
		}
	}

	return m, nil
}

// textEdit is a caret position inside a rune buffer. Both the field editor and
// the ad-hoc prompt drive their key handling through it.
type textEdit struct {
	value string
	pos   int
}

func newTextEdit(value string, pos int) textEdit {
	e := textEdit{value: value}
	return e.withCursor(pos)
}

func (e textEdit) runes() []rune { return []rune(e.value) }

func (e textEdit) withCursor(pos int) textEdit {
	n := len(e.runes())
	if pos < 0 {
		pos = 0
	}
	if pos > n {
		pos = n
	}
	e.pos = pos
	return e
}

func (e textEdit) insert(s string) textEdit {
	r := e.runes()
	e.value = string(r[:e.pos]) + s + string(r[e.pos:])
	e.pos += len([]rune(s))
	return e
}

func (e textEdit) deleteRange(from, to int) textEdit {
	r := e.runes()
	if from < 0 {
		from = 0
	}
	if to > len(r) {
		to = len(r)
	}
	if from >= to {
		return e
	}
	e.value = string(r[:from]) + string(r[to:])
	e.pos = from
	return e
}

func (e textEdit) wordStart() int {
	r := e.runes()
	i := e.pos
	for i > 0 && r[i-1] == ' ' {
		i--
	}
	for i > 0 && r[i-1] != ' ' {
		i--
	}
	return i
}

// apply maps a key to an edit. Keys that end editing (enter, esc) are left to
// the caller, which is why they are reported as unhandled.
func (e textEdit) apply(msg tea.KeyMsg) (textEdit, bool) {
	switch msg.Type {
	case tea.KeyLeft, tea.KeyCtrlB:
		return e.withCursor(e.pos - 1), true
	case tea.KeyRight, tea.KeyCtrlF:
		return e.withCursor(e.pos + 1), true
	case tea.KeyHome, tea.KeyCtrlA:
		return e.withCursor(0), true
	case tea.KeyEnd, tea.KeyCtrlE:
		return e.withCursor(len(e.runes())), true
	case tea.KeyBackspace:
		return e.deleteRange(e.pos-1, e.pos), true
	case tea.KeyDelete, tea.KeyCtrlD:
		return e.deleteRange(e.pos, e.pos+1), true
	case tea.KeyCtrlW:
		return e.deleteRange(e.wordStart(), e.pos), true
	case tea.KeyCtrlU:
		return e.deleteRange(0, e.pos), true
	case tea.KeyCtrlK:
		return e.deleteRange(e.pos, len(e.runes())), true
	case tea.KeySpace:
		return e.insert(" "), true
	case tea.KeyRunes:
		return e.insert(string(msg.Runes)), true
	}
	return e, false
}

func (m TicketViewModel) handleEditKey(msg tea.KeyMsg) (TicketViewModel, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		if m.ticket != nil && m.cursor < len(m.fields) {
			f := m.fields[m.cursor]
			if f.set != nil {
				f.set(m.ticket, m.editBuffer)
				m.saveTicket()
			}
		}
		m.mode = ticketViewMode
		m.editBuffer = ""
		m.editCursor = 0
		return m, nil
	case tea.KeyEscape:
		m.mode = ticketViewMode
		m.editBuffer = ""
		m.editCursor = 0
		return m, nil
	}

	if e, handled := newTextEdit(m.editBuffer, m.editCursor).apply(msg); handled {
		m.editBuffer = e.value
		m.editCursor = e.pos
	}

	return m, nil
}

func (m TicketViewModel) handleBranchModeSelectKey(msg tea.KeyMsg) (TicketViewModel, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.mode = ticketViewMode
		return m, nil
	case tea.KeyEnter:
		if m.ticket == nil {
			m.mode = ticketViewMode
			return m, nil
		}
		mode := branchModeOptions[m.branchModeCursor]
		updated, err := m.board.SetBranchMode(context.Background(), m.ticket.ID, mode)
		if err != nil {
			m.mode = ticketViewMode
			return m, func() tea.Msg {
				return notificationMsg{title: "Error", message: err.Error(), variant: NotificationError}
			}
		}
		*m.ticket = updated
		if mode == store.BranchModeCurrent {
			m.mode = ticketViewMode
			return m, nil
		}
		m.editBuffer = m.ticket.Branch
		m.editCursor = len([]rune(m.editBuffer))
		m.mode = ticketEditMode
		return m, nil
	case tea.KeyUp:
		if m.branchModeCursor > 0 {
			m.branchModeCursor--
		}
		return m, nil
	case tea.KeyDown:
		if m.branchModeCursor < len(branchModeOptions)-1 {
			m.branchModeCursor++
		}
		return m, nil
	}

	if msg.Type == tea.KeyRunes {
		switch string(msg.Runes) {
		case "j":
			if m.branchModeCursor < len(branchModeOptions)-1 {
				m.branchModeCursor++
			}
		case "k":
			if m.branchModeCursor > 0 {
				m.branchModeCursor--
			}
		}
	}

	return m, nil
}

func (m TicketViewModel) handleAgentSelectKey(msg tea.KeyMsg) (TicketViewModel, tea.Cmd) {
	total := len(m.agents) + 1

	switch msg.Type {
	case tea.KeyEscape:
		m.mode = ticketViewMode
		return m, nil
	case tea.KeyEnter:
		if m.ticket != nil {
			if m.agentCursor == 0 {
				m.ticket.Agent = ""
			} else {
				idx := m.agentCursor - 1
				if idx < len(m.agents) {
					m.ticket.Agent = m.agents[idx].Name
				}
			}
			m.saveTicket()
			agent := m.ticket.Agent
			ticketID := m.ticket.ID
			m.mode = ticketViewMode
			return m, func() tea.Msg {
				return agentAssignedMsg{ticketID: ticketID, agent: agent}
			}
		}
		m.mode = ticketViewMode
		return m, nil
	case tea.KeyUp:
		if m.agentCursor > 0 {
			m.agentCursor--
		}
		return m, nil
	case tea.KeyDown:
		if m.agentCursor < total-1 {
			m.agentCursor++
		}
		return m, nil
	}

	if msg.Type == tea.KeyRunes {
		switch string(msg.Runes) {
		case "j":
			if m.agentCursor < total-1 {
				m.agentCursor++
			}
		case "k":
			if m.agentCursor > 0 {
				m.agentCursor--
			}
		}
	}

	return m, nil
}

func (m TicketViewModel) handlePrioritySelectKey(msg tea.KeyMsg) (TicketViewModel, tea.Cmd) {
	total := len(m.priorities)

	switch msg.Type {
	case tea.KeyEscape:
		m.mode = ticketViewMode
		return m, nil
	case tea.KeyEnter:
		if m.ticket != nil && m.priorityCursor < len(m.priorities) {
			m.ticket.Priority = m.priorities[m.priorityCursor]
			m.saveTicket()
		}
		m.mode = ticketViewMode
		return m, nil
	case tea.KeyUp:
		if m.priorityCursor > 0 {
			m.priorityCursor--
		}
		return m, nil
	case tea.KeyDown:
		if m.priorityCursor < total-1 {
			m.priorityCursor++
		}
		return m, nil
	}

	if msg.Type == tea.KeyRunes {
		switch string(msg.Runes) {
		case "j":
			if m.priorityCursor < total-1 {
				m.priorityCursor++
			}
		case "k":
			if m.priorityCursor > 0 {
				m.priorityCursor--
			}
		}
	}

	return m, nil
}

func (m TicketViewModel) handleDependsOnSelectKey(msg tea.KeyMsg) (TicketViewModel, tea.Cmd) {
	total := len(m.dependsOnTickets)

	switch msg.Type {
	case tea.KeyEscape:
		m.mode = ticketViewMode
		return m, nil
	case tea.KeyEnter:
		if m.ticket != nil {
			m.ticket.DependsOn = m.dependsOnSelected
			m.saveTicket()
		}
		m.mode = ticketViewMode
		return m, nil
	case tea.KeySpace:
		if m.dependsOnCursor < len(m.dependsOnTickets) {
			id := m.dependsOnTickets[m.dependsOnCursor].ID
			if m.ticket != nil && id == m.ticket.ID {
				return m, nil
			}
			found := false
			for i, d := range m.dependsOnSelected {
				if d == id {
					m.dependsOnSelected = append(m.dependsOnSelected[:i], m.dependsOnSelected[i+1:]...)
					found = true
					break
				}
			}
			if !found {
				m.dependsOnSelected = append(m.dependsOnSelected, id)
			}
		}
		return m, nil
	case tea.KeyUp:
		if m.dependsOnCursor > 0 {
			m.dependsOnCursor--
		}
		return m, nil
	case tea.KeyDown:
		if m.dependsOnCursor < total-1 {
			m.dependsOnCursor++
		}
		return m, nil
	}

	if msg.Type == tea.KeyRunes {
		switch string(msg.Runes) {
		case "j":
			if m.dependsOnCursor < total-1 {
				m.dependsOnCursor++
			}
		case "k":
			if m.dependsOnCursor > 0 {
				m.dependsOnCursor--
			}
		}
	}

	return m, nil
}

func (m TicketViewModel) handleAdHocPromptKey(msg tea.KeyMsg) (TicketViewModel, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.mode = ticketViewMode
		m.adhocPrompt = ""
		m.adhocPromptCursor = 0
		return m, nil
	case tea.KeyEnter:
		if m.adhocPrompt != "" {
			agent := m.adhocAgent
			prompt := m.adhocPrompt
			m.mode = ticketViewMode
			m.adhocPrompt = ""
			m.adhocPromptCursor = 0
			return m, func() tea.Msg {
				return adhocRunStartedMsg{agent: agent, prompt: prompt}
			}
		}
		return m, nil
	}

	if e, handled := newTextEdit(m.adhocPrompt, m.adhocPromptCursor).apply(msg); handled {
		m.adhocPrompt = e.value
		m.adhocPromptCursor = e.pos
	}

	return m, nil
}

func (m TicketViewModel) cycleStatus() (TicketViewModel, tea.Cmd) {
	currentIdx := -1
	for i, s := range statusCycle {
		if s == m.ticket.Status {
			currentIdx = i
			break
		}
	}
	if currentIdx == -1 {
		currentIdx = 0
	}
	nextIdx := (currentIdx + 1) % len(statusCycle)
	newStatus := statusCycle[nextIdx]
	ticketID := m.ticket.ID

	return m, func() tea.Msg {
		return statusChangedMsg{ticketID: ticketID, newStatus: newStatus}
	}
}

func (m TicketViewModel) buildPromptFromTicket() string {
	if m.ticket == nil {
		return ""
	}
	// The agent gets the prompt the user wrote, nothing else. The ticket id and
	// title are board bookkeeping and only crowd the agent's composer.
	return m.ticket.Prompt
}

func (m TicketViewModel) SetTicket(t *store.Ticket) TicketViewModel {
	m.ticket = t
	m.cursor = 0
	m.mode = ticketViewMode
	m.editBuffer = ""
	m.editCursor = 0
	m.activeProposal = nil
	return m
}

func (m TicketViewModel) SetProposal(p *store.Proposal) TicketViewModel {
	m.activeProposal = p
	m.loading = false
	return m
}

func (m TicketViewModel) SetLoading(loading bool) TicketViewModel {
	m.loading = loading
	return m
}

func truncateText(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		return string(r[:width])
	}
	return string(r[:width-1]) + "…"
}

func (m TicketViewModel) statusColor(status string) lipgloss.Color {
	if c, ok := m.styles.StatusColors[status]; ok {
		return c
	}
	return m.styles.MutedColor
}

func (m TicketViewModel) priorityColor(priority string) lipgloss.Color {
	if c, ok := m.styles.PriorityColors[priority]; ok {
		return c
	}
	return m.styles.MutedColor
}

func (m TicketViewModel) pill(text string, bg lipgloss.Color) string {
	return lipgloss.NewStyle().
		Foreground(m.styles.OnAccent).
		Background(bg).
		Bold(true).
		Padding(0, 1).
		Render(text)
}

func (m TicketViewModel) rule(width int, glyph string) string {
	if width < 1 {
		width = 1
	}
	return m.styles.Rule.Render(strings.Repeat(glyph, width))
}

func (m TicketViewModel) header(innerWidth int) string {
	badge := m.styles.Badge.Render(m.ticket.ID)
	status := m.pill(m.ticket.Status, m.statusColor(m.ticket.Status))

	room := innerWidth - lipgloss.Width(badge) - lipgloss.Width(status) - 4
	title := m.styles.Title.Render(truncateText(m.ticket.Title, room))

	left := lipgloss.JoinHorizontal(lipgloss.Top, badge, "  ", title)
	gap := innerWidth - lipgloss.Width(left) - lipgloss.Width(status)
	if gap < 1 {
		gap = 1
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), status)
}

func (m TicketViewModel) chips(items []string) string {
	if len(items) == 0 {
		return ""
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, m.styles.Value.Render(it))
	}
	return strings.Join(out, m.styles.Rule.Render(" · "))
}

func (m TicketViewModel) fieldValue(f ticketField, width int, selected bool) string {
	raw := f.value(m.ticket)

	switch f.label {
	case "Status":
		return lipgloss.NewStyle().Foreground(m.statusColor(raw)).Bold(true).Render("● " + raw)
	case "Priority":
		if raw == "" {
			break
		}
		return lipgloss.NewStyle().Foreground(m.priorityColor(raw)).Bold(true).Render(raw)
	case "Agent":
		if raw == "" {
			return m.styles.Empty.Render("unassigned")
		}
		return lipgloss.NewStyle().Foreground(m.styles.AccentColor).Render(raw)
	case "Branch":
		return m.branchDisplay()
	case "Tags":
		return m.chips(m.ticket.Tags)
	case "Depends On":
		return m.chips(m.ticket.DependsOn)
	case "ID", "Created", "Updated", "Resume":
		if raw != "" {
			return m.styles.Empty.Render(raw)
		}
	}

	if raw == "" {
		return m.styles.Empty.Render("—")
	}

	style := m.styles.Value.Copy().Width(width)
	if selected {
		style = style.Bold(true)
	}
	return style.Render(raw)
}

func (m TicketViewModel) fieldRow(i int, f ticketField, innerWidth int) string {
	selected := i == m.cursor
	const labelWidth = 12

	valWidth := innerWidth - labelWidth - 4
	if valWidth < 10 {
		valWidth = 10
	}

	var prefix, label string
	if selected {
		prefix = m.styles.Cursor.Render("▸ ")
		label = m.styles.LabelActive.Render(fmt.Sprintf("%-*s", labelWidth-2, f.label))
	} else {
		prefix = "  "
		label = m.styles.Label.Render(fmt.Sprintf("%-*s", labelWidth, f.label))
	}

	value := m.fieldValue(f, valWidth, selected)

	return lipgloss.JoinHorizontal(lipgloss.Top, prefix, label, " ", value)
}

func (m TicketViewModel) inputLine(buffer string, pos int, innerWidth int) string {
	width := innerWidth - 4
	if width < 10 {
		width = 10
	}

	r := []rune(buffer)
	if pos > len(r) {
		pos = len(r)
	}
	if pos < 0 {
		pos = 0
	}

	avail := width - 1
	// Keep a little of the text after the caret on screen when scrolled.
	margin := avail / 3
	if margin > 8 {
		margin = 8
	}
	start := pos - avail + margin
	if maxStart := len(r) - avail; start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}
	end := start + avail
	if end > len(r) {
		end = len(r)
	}

	seg := r[start:end]
	caret := pos - start
	under := " "
	after := ""
	if caret < len(seg) {
		under = string(seg[caret])
		after = string(seg[caret+1:])
	}

	text := m.styles.Value.Copy().Bold(true)
	line := text.Render(string(seg[:caret])) +
		m.styles.CaretCell.Render(under) +
		text.Render(after)
	if start > 0 {
		line = m.styles.Empty.Render("…") + line
	}

	return lipgloss.JoinVertical(lipgloss.Left, line, m.rule(width, "─"))
}

func (m TicketViewModel) panel(title string, body []string, innerWidth int) string {
	width := innerWidth - 2
	if width < 16 {
		width = 16
	}
	content := m.styles.PanelTitle.Render(title)
	if len(body) > 0 {
		content += "\n" + strings.Join(body, "\n")
	}
	return m.styles.Panel.Copy().Width(width).Render(content)
}

func (m TicketViewModel) option(label string, selected bool) string {
	if selected {
		return m.styles.Cursor.Render("▸ ") + m.styles.OptionActive.Render(label)
	}
	return "  " + m.styles.Value.Render(label)
}

func (m TicketViewModel) View() string {
	if m.ticket == nil {
		return m.styles.Empty.Render("No ticket selected")
	}

	if m.width == 0 {
		return ""
	}

	innerWidth := m.width - 6
	if innerWidth < 20 {
		innerWidth = 20
	}

	var b strings.Builder

	b.WriteString(m.header(innerWidth))
	b.WriteString("\n")
	b.WriteString(m.rule(innerWidth, "━"))
	b.WriteString("\n")

	groupEnds := map[string]bool{"Prompt": true, "Branch": true}
	for i, f := range m.fields {
		b.WriteString(m.fieldRow(i, f, innerWidth))
		b.WriteString("\n")
		if groupEnds[f.label] {
			b.WriteString(m.rule(innerWidth, "┈"))
			b.WriteString("\n")
		}
	}

	if m.loading || m.activeProposal != nil || m.ticket.Status == "in_progress" {
		var body []string
		switch {
		case m.loading:
			body = append(body, m.pill("generating", m.styles.WarningColor)+
				m.styles.Empty.Render("  please wait"))
		case m.activeProposal != nil:
			color := m.styles.MutedColor
			switch m.activeProposal.Status {
			case "pending":
				color = m.styles.AccentColor
			case "approved":
				color = m.styles.SuccessColor
			}
			body = append(body, m.pill(m.activeProposal.Status, color), "")

			preview := m.activeProposal.Prompt
			if len(preview) > 300 {
				preview = preview[:300] + "\n\n" + m.styles.Empty.Render("[…] press v for the full proposal")
			}
			body = append(body, m.styles.Value.Copy().Width(innerWidth-8).Render(preview))
		default:
			body = append(body, m.styles.Empty.Render("No active proposal for this ticket."))
		}
		b.WriteString("\n")
		b.WriteString(m.panel("Active Proposal", body, innerWidth))
		b.WriteString("\n")
	}

	switch m.mode {
	case ticketEditMode:
		b.WriteString("\n")
		b.WriteString(m.panel("Edit "+m.fields[m.cursor].label, []string{
			m.inputLine(m.editBuffer, m.editCursor, innerWidth),
		}, innerWidth))
		b.WriteString("\n")

	case ticketBranchModeSelectMode:
		body := make([]string, 0, len(branchModeOptions))
		for i, mode := range branchModeOptions {
			label := branchModeLabel(mode)
			hint := "isolated worktree"
			if mode == store.BranchModeCurrent {
				hint = "no worktree"
				if m.currentBranch != "" {
					hint = m.currentBranch
				}
			}
			line := m.option(label, i == m.branchModeCursor) +
				m.styles.Empty.Render("  "+hint)
			body = append(body, line)
		}
		b.WriteString("\n")
		b.WriteString(m.panel("Work on:", body, innerWidth))
		b.WriteString("\n")

	case ticketAgentSelectMode:
		items := make([]string, 0, len(m.agents)+1)
		items = append(items, "None")
		for _, ag := range m.agents {
			items = append(items, ag.Name)
		}
		body := make([]string, 0, len(items))
		for i, item := range items {
			body = append(body, m.option(item, i == m.agentCursor))
		}
		b.WriteString("\n")
		b.WriteString(m.panel("Select Agent:", body, innerWidth))
		b.WriteString("\n")

	case ticketPrioritySelectMode:
		body := make([]string, 0, len(m.priorities))
		for i, p := range m.priorities {
			display := p
			if display == "" {
				display = "None"
			}
			body = append(body, m.option(display, i == m.priorityCursor))
		}
		b.WriteString("\n")
		b.WriteString(m.panel("Select Priority:", body, innerWidth))
		b.WriteString("\n")

	case ticketDependsOnSelectMode:
		var body []string
		for i, t := range m.dependsOnTickets {
			if t.ID == m.ticket.ID {
				continue
			}
			mark := m.styles.Empty.Render("○")
			for _, d := range m.dependsOnSelected {
				if d == t.ID {
					mark = lipgloss.NewStyle().Foreground(m.styles.SuccessColor).Render("●")
					break
				}
			}
			label := t.ID + "  " + truncateText(t.Title, innerWidth-20)
			body = append(body, m.option(mark+" "+label, i == m.dependsOnCursor))
		}
		if len(body) == 0 {
			body = []string{m.styles.Empty.Render("No tickets available")}
		}
		b.WriteString("\n")
		b.WriteString(m.panel("Select Dependencies (space to toggle):", body, innerWidth))
		b.WriteString("\n")

	case ticketAdHocPromptMode:
		b.WriteString("\n")
		b.WriteString(m.panel(fmt.Sprintf("Run %s with prompt:", m.adhocAgent), []string{
			m.inputLine(m.adhocPrompt, m.adhocPromptCursor, innerWidth),
		}, innerWidth))
		b.WriteString("\n")
	}

	var footer string
	switch m.mode {
	case ticketBranchModeSelectMode, ticketAgentSelectMode, ticketPrioritySelectMode:
		footer = "↑/k: up │ ↓/j: down │ Enter: select │ Esc: cancel"
	case ticketDependsOnSelectMode:
		footer = "↑/k: up │ ↓/j: down │ Space: toggle │ Enter: save │ Esc: cancel"
	case ticketAdHocPromptMode:
		footer = "Enter: run agent │ Esc: cancel"
	case ticketEditMode:
		footer = "Enter: save │ Esc: cancel"
	default:
		footer = m.helpFooter()
		if m.activeProposal != nil {
			footer += " │ v: view proposal"
		}
		if m.activeProposal != nil && m.activeProposal.Status == "pending" {
			footer += " │ o: approve proposal"
		}
	}

	h := m.height - 7
	if h < 0 {
		h = 0
	}
	if m.viewport.Width == 0 {
		m.viewport.Width = innerWidth
	}
	if m.viewport.Height == 0 {
		m.viewport.Height = h
	}

	m.viewport.SetContent(b.String())
	if m.mode != ticketViewMode {
		m.viewport.GotoBottom()
	}

	footerRendered := lipgloss.JoinVertical(lipgloss.Left,
		m.rule(innerWidth, "─"),
		m.styles.Footer.Render(truncateText(footer, innerWidth)),
	)
	finalView := lipgloss.JoinVertical(lipgloss.Left, m.viewport.View(), footerRendered)
	return m.styles.Border.Render(finalView)
}

func parseTags(input string) []string {
	if input == "" {
		return nil
	}
	tags := strings.Split(input, ",")
	result := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t != "" {
			result = append(result, t)
		}
	}
	return result
}
