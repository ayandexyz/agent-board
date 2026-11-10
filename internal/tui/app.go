package tui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ayandexyz/agent-board/internal/activity"
	"github.com/ayandexyz/agent-board/internal/config"
	"github.com/ayandexyz/agent-board/internal/keybinding"
	"github.com/ayandexyz/agent-board/internal/orchestrator"
	"github.com/ayandexyz/agent-board/internal/store"
	"github.com/ayandexyz/agent-board/internal/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var debugLog = log.New(os.Stderr, "[agentboard] ", log.Ltime)

type focusArea int

const (
	focusBoard focusArea = iota
	focusAgentPane
)

type viewMode int

const (
	viewBoard viewMode = iota
	viewTicket
	viewHelp
	viewDashboard
)

type editorFinishedMsg struct {
	err error
}

type statusChangedMsg struct {
	ticketID  string
	newStatus string
}

type proposalCreatedMsg struct {
	proposal store.Proposal
}

type proposalApprovedMsg struct {
	proposalID string
}

type runStartedMsg struct {
	proposalID string
}

type directRunStartedMsg struct {
	ticketID string
	agent    string
	prompt   string
}

type adhocRunStartedMsg struct {
	agent  string
	prompt string
}

type runStartFailedMsg struct {
	err error
}

type runCompletedMsg struct {
	ticketID  string
	sessionID string
}

type agentActiveChangedMsg struct{}

type ticketMovedMsg struct {
	from, to, repo string
}

// boardSyncMsg rereads the board. Another agentboard process may share these
// tickets: the board of the repository a workspace ticket belongs to, or the
// workspace showing this repository's tickets.
type boardSyncMsg struct{}

const boardSyncInterval = 2 * time.Second

func boardSyncCmd() tea.Cmd {
	return tea.Tick(boardSyncInterval, func(time.Time) tea.Msg { return boardSyncMsg{} })
}

type proposalFailedMsg struct {
	ticketID string
	err      error
}

type proposalLoadedMsg struct {
	TicketID string
	proposal *store.Proposal
}

type generateProposalMsg struct {
	ticketID string
}

type viewProposalFullMsg struct {
	proposalID string
}

type notificationMsg struct {
	title   string
	message string
	variant NotificationVariant
}

type deleteTicketConfirmMsg struct {
	ticketID string
}

type deleteTicketRequestMsg struct {
	ticketID string
}

type showDeleteModalMsg struct {
	ticketID string
}

type tabChangeMsg struct {
	tab KanbanTab
}

type searchQueryMsg struct {
	query string
}

type searchResultsMsg struct {
	tickets []store.Ticket
}

type monthNavigateMsg struct {
	direction int
}

// Board is the ticket side of the container: every read and write of
// tickets the views make goes through it, so the rules live in one place
// for the TUI and any other front end.
type Board interface {
	ListTickets(ctx context.Context, f store.TicketFilters) ([]store.Ticket, error)
	GetTicket(ctx context.Context, id string) (store.Ticket, error)
	CreateTicket(ctx context.Context, in orchestrator.CreateTicketInput) (store.Ticket, error)
	UpdateTicket(ctx context.Context, t store.Ticket) (store.Ticket, error)
	SetBranchMode(ctx context.Context, ticketID, mode string) (store.Ticket, error)
	SetTicketRepo(ctx context.Context, ticketID, repo string) (store.Ticket, error)
	SetTicketPrefix(prefix string)
	ProjectInitDate(ctx context.Context) (time.Time, error)
	GetProposal(ctx context.Context, id string) (store.Proposal, error)
	GetActiveProposalForTicket(ctx context.Context, ticketID string) (store.Proposal, error)
	ListActiveSessions(ctx context.Context) ([]store.Session, error)
}

type Orchestrator interface {
	Board
	CreateProposal(ctx context.Context, input orchestrator.CreateProposalInput) (store.Proposal, error)
	ApproveProposal(ctx context.Context, proposalID string) error
	StartApprovedRun(ctx context.Context, proposalID string) (store.Session, error)
	StartAdHocRun(ctx context.Context, ticketID, agent, prompt string) (store.Session, error)
	MoveTicketStatus(ctx context.Context, ticketID, newStatus string) error
	CompleteTicket(ctx context.Context, ticketID string) error
	MergeTicket(ctx context.Context, ticketID string, strategy orchestrator.MergeStrategy) (orchestrator.MergeResult, error)
	DeleteTicket(ctx context.Context, ticketID string) error
	FinishRun(ctx context.Context, input orchestrator.FinishRunInput) error
	GetLogs(sessionID string) []string
	SendInput(sessionID, input string) error
	GetActiveSessions() []*orchestrator.AgentSession
	GetPaneContent(sessionID string, lines int) (string, error)
	SwitchToPane(sessionID string) error
	SubscribeCompletions() (<-chan orchestrator.RunCompletion, func())
}

// ActivityReader is what the board asks about agent state. The hook receiver
// satisfies it; nil means no receiver is running and cards show no glyph.
type ActivityReader interface {
	StateOf(sessionID string) activity.State
	Waiting() []activity.Session
}

type AppDeps struct {
	Orchestrator Orchestrator
	// Activity answers "what is this session doing" for the card glyphs and
	// the waiting overlay. Nil when no receiver is running.
	Activity ActivityReader
	// ActivityEvents delivers "agent needs you" alerts from the hook receiver.
	// Nil when no receiver is running.
	ActivityEvents <-chan activity.Alert
	// ResolvePermission answers a held permission request: "allow", "deny", or
	// "" to leave the choice to the agent's own picker in the pane. Nil when no
	// receiver is running.
	ResolvePermission func(sessionID, decision string)
	// CurrentBranch is the branch the repository is checked out on, shown as
	// the "current branch" choice on a ticket. Empty outside a git repo.
	CurrentBranch string
}

type App struct {
	orchestrator Orchestrator
	resolver     *keybinding.Resolver
	config       *config.Config
	registry     *theme.Registry
	width        int
	height       int

	focus        focusArea
	view         viewMode
	palette      CommandPalette
	modal        ConfirmModal
	mergeModal   MergeModal
	textInput    TextInputModal
	notification NotificationStack

	activityEvents    <-chan activity.Alert
	resolvePermission func(sessionID, decision string)
	activityReader    ActivityReader
	activityByTicket  map[string]string
	waitingCount      int
	waiting           WaitingOverlay

	quit       bool
	runCommand tea.Cmd

	kanban       KanbanModel
	ticketView   TicketViewModel
	dashboard    DashboardModel
	activeTicket *store.Ticket

	generatingProposals map[string]bool
	completionCh        <-chan orchestrator.RunCompletion
	unsubscribe         func()
}

func NewApp(cfg *config.Config, reg *theme.Registry, deps AppDeps) (*App, error) {
	km := keybinding.DefaultKeyMap()
	if len(cfg.TUI.Keybindings) > 0 {
		keybinding.ApplyConfig(&km, cfg.TUI.Keybindings)
	}

	resolver := keybinding.NewResolver(km)

	t := reg.Active()
	kanban, err := NewKanbanModel(deps.Orchestrator, resolver, t, cfg.Board.Columns)
	if err != nil {
		return nil, fmt.Errorf("tui.newApp: %w", err)
	}

	if initDate, err := deps.Orchestrator.ProjectInitDate(context.Background()); err == nil {
		kanban.projectInitDate = initDate
		kanban.monthOffset = computeOffsetToContain(time.Now(), initDate)
		kanban, err = kanban.Reload()
		if err != nil {
			return nil, fmt.Errorf("tui.newApp: %w", err)
		}
	}

	agents := config.DetectAgents()
	completions, unsubscribe := deps.Orchestrator.SubscribeCompletions()

	a := &App{
		orchestrator:        deps.Orchestrator,
		activityEvents:      deps.ActivityEvents,
		resolvePermission:   deps.ResolvePermission,
		activityReader:      deps.Activity,
		activityByTicket:    map[string]string{},
		resolver:            resolver,
		config:              cfg,
		registry:            reg,
		focus:               focusBoard,
		view:                viewBoard,
		kanban:              kanban,
		ticketView:          NewTicketViewModel(deps.Orchestrator, resolver, t, agents).SetGitContext(deps.CurrentBranch, cfg.Git.BranchPrefix),
		dashboard:           NewDashboardModel(deps.Orchestrator, resolver, agents, t),
		generatingProposals: make(map[string]bool),
		completionCh:        completions,
		unsubscribe:         unsubscribe,
	}

	cr := NewCommandRegistry()
	ac := newAppCommands(a, reg, cfg)
	ac.registerAll(cr)

	a.palette = NewCommandPalette(cr, nil)
	a.palette.SetTheme(t)
	a.palette.onSelect = ac.onSelect
	a.palette.onConfirm = ac.onConfirm

	a.modal = ConfirmModal{}
	a.modal.SetTheme(t)
	a.mergeModal = MergeModal{}
	a.mergeModal.SetTheme(t)
	a.textInput = TextInputModal{}
	a.textInput.SetTheme(t)
	a.notification = NotificationStack{}
	a.notification.SetTheme(t)
	a.waiting = WaitingOverlay{}
	a.waiting.SetTheme(t)

	return a, nil
}

func (a *App) applyTheme() {
	t := a.registry.Active()
	a.kanban.styles = NewKanbanStyles(t)
	a.kanban.theme = t
	a.ticketView.styles = NewTicketViewStyles(t)
	a.dashboard.styles = NewDashboardStyles(t)
	a.palette.SetTheme(t)
	a.modal.SetTheme(t)
	a.mergeModal.SetTheme(t)
	a.textInput.SetTheme(t)
	a.notification.SetTheme(t)
	a.waiting.SetTheme(t)
}

// refreshActivity rebuilds the per-ticket state the cards show and the count
// in the header from the live sessions and what the receiver knows about
// each. Cheap enough to run on every animation tick: a few map lookups.
func (a *App) refreshActivity() {
	byTicket := map[string]string{}
	waiting := 0
	if a.activityReader != nil {
		for _, sess := range a.orchestrator.GetActiveSessions() {
			state := a.activityReader.StateOf(sess.SessionID)
			if state == "" {
				continue
			}
			if state.NeedsInput() {
				waiting++
			}
			if sess.TicketID != "" {
				byTicket[sess.TicketID] = string(state)
			}
		}
	}
	a.activityByTicket = byTicket
	a.waitingCount = waiting
	a.kanban = a.kanban.WithActivity(byTicket)
}

// waitingItems is what the overlay lists: every session the receiver says
// needs a human, named by its ticket where it has one.
func (a *App) waitingItems() []WaitingItem {
	if a.activityReader == nil {
		return nil
	}
	ticketOf := map[string]string{}
	for _, sess := range a.orchestrator.GetActiveSessions() {
		ticketOf[sess.SessionID] = sess.TicketID
	}
	var items []WaitingItem
	for _, s := range a.activityReader.Waiting() {
		items = append(items, WaitingItem{
			SessionID: s.ID,
			TicketID:  ticketOf[s.ID],
			State:     string(s.State),
			ToolName:  s.ToolName,
			Detail:    s.Detail,
			Decidable: s.Decidable && a.resolvePermission != nil,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].TicketID != items[j].TicketID {
			return items[i].TicketID < items[j].TicketID
		}
		return items[i].SessionID < items[j].SessionID
	})
	return items
}

// reopenWaiting refreshes the overlay after a decision and closes it once
// nothing is left to decide.
func (a *App) reopenWaiting() {
	items := a.waitingItems()
	if len(items) == 0 {
		a.waiting.Close()
		return
	}
	a.waiting.Open(items)
}

// openPermissionModal asks the user to approve the tool call an agent is
// blocked on. Cancelling denies: a permission dialog left unanswered stalls the
// agent for the whole hook timeout, so the modal fails closed.
func (a *App) openPermissionModal(alert activity.Alert) {
	sessionID := alert.SessionID
	resolve := a.resolvePermission
	a.modal.Open(
		alert.Title,
		alert.Body,
		func() tea.Cmd {
			resolve(sessionID, "allow")
			return nil
		},
		func() { resolve(sessionID, "deny") },
	)
}

// activityNotificationDuration is how long an "agent needs you" card stays up.
const activityNotificationDuration = 20 * time.Second

// waitForActivityCmd blocks on the next agent alert and re-arms itself, the
// same shape as the run-completion listener.
func (a *App) waitForActivityCmd() tea.Cmd {
	if a.activityEvents == nil {
		return nil
	}
	return func() tea.Msg {
		alert, ok := <-a.activityEvents
		if !ok {
			return nil
		}
		return alert
	}
}

// Close ends the app's subscriptions so their listeners unblock.
func (a *App) Close() {
	if a.unsubscribe != nil {
		a.unsubscribe()
	}
}

// waitForCompletionCmd blocks on the next run completion, whichever run it
// belongs to, and re-arms itself from the runCompletedMsg handler. One
// listener for the whole board: a per-run listener could take another run's
// completion and report it under its own ticket.
func (a *App) waitForCompletionCmd() tea.Cmd {
	if a.completionCh == nil {
		return nil
	}
	return func() tea.Msg {
		completion, ok := <-a.completionCh
		if !ok {
			return nil
		}
		return runCompletedMsg{ticketID: completion.TicketID, sessionID: completion.SessionID}
	}
}

func (a *App) Init() tea.Cmd {
	// Always arm the animation tick at startup. The kanban only advances
	// its animFrame when an agent is active, so idle ticks are cheap.
	cmds := []tea.Cmd{tea.EnterAltScreen, boardSyncCmd()}
	if c := a.tickCmd(); c != nil {
		cmds = append(cmds, c)
	}
	if c := a.waitForActivityCmd(); c != nil {
		cmds = append(cmds, c)
	}
	if c := a.waitForCompletionCmd(); c != nil {
		cmds = append(cmds, c)
	}
	return tea.Batch(cmds...)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.kanban, _ = a.kanban.Update(msg)
		a.ticketView, _ = a.ticketView.Update(msg)
		a.dashboard, _ = a.dashboard.Update(msg)
		a.palette, _ = a.palette.Update(msg)
		a.modal.SetSize(a.width, a.height)
		a.mergeModal.SetSize(a.width, a.height)
		a.textInput.SetSize(a.width, a.height)
		a.notification.SetSize(a.width, a.height)
		a.waiting.SetSize(a.width, a.height)
		return a, nil
	case tea.KeyMsg:
		return a.handleKey(msg)
	case editorFinishedMsg:
		return a, nil
	case tickMsg:
		var cmd tea.Cmd
		a.kanban, cmd = a.kanban.Update(msg)
		a.dashboard, _ = a.dashboard.Update(msg)
		a.refreshActivity()

		return a, cmd
	case notificationDismissMsg:
		a.notification = a.notification.HandleDismiss(msg)
		return a, nil
	case ticketCreatedMsg:
		return a, a.showNotification(
			"Ticket created",
			fmt.Sprintf("%s: %s", msg.id, msg.title),
			NotificationSuccess,
		)
	case agentAssignedMsg:
		message := fmt.Sprintf("%s cleared on %s", "Agent", msg.ticketID)
		if msg.agent != "" {
			message = fmt.Sprintf("%s assigned to %s", msg.agent, msg.ticketID)
		}
		return a, a.showNotification(
			"Agent assignment updated",
			message,
			NotificationSuccess,
		)
	case statusChangedMsg:
		return a.handleStatusChanged(msg)
	case proposalCreatedMsg:
		return a, tea.Batch(
			a.showNotification(
				"Proposal created",
				fmt.Sprintf("AI proposed work for %s", msg.proposal.TicketID),
				NotificationInfo,
			),
			a.loadProposalCmd(msg.proposal.TicketID),
		)
	case proposalLoadedMsg:
		delete(a.generatingProposals, msg.TicketID)
		if a.activeTicket != nil && a.activeTicket.ID == msg.TicketID {
			a.ticketView = a.ticketView.SetProposal(msg.proposal)
		}
		return a, nil
	case generateProposalMsg:
		return a.handleGenerateProposal(msg)
	case proposalApprovedMsg:
		return a, a.approveProposalCmd(msg.proposalID)
	case viewProposalFullMsg:
		p, err := a.orchestrator.GetProposal(context.Background(), msg.proposalID)
		if err != nil {
			return a, nil
		}
		a.modal.OpenInfo("Full Proposal for "+p.TicketID, p.Prompt, nil)
		return a, nil
	case runStartedMsg:
		a.kanban, _ = a.kanban.Reload()
		return a, tea.Batch(
			a.showNotification("Run started", "Agent is working...", NotificationInfo),
			a.startRunAndListenCmd(msg.proposalID),
			a.tickCmd(),
		)
	case adhocRunStartedMsg:
		a.kanban, _ = a.kanban.Reload()
		return a, tea.Batch(
			a.showNotification("Ad-hoc run started", fmt.Sprintf("%s is working...", msg.agent), NotificationInfo),
			a.startAdHocRunAndListenCmd(msg.agent, msg.prompt),
			a.tickCmd(),
		)
	case directRunStartedMsg:
		a.kanban, _ = a.kanban.Reload()
		return a, tea.Batch(
			a.showNotification("Run started", "Agent is working...", NotificationInfo),
			a.startDirectRunAndListenCmd(msg.ticketID, msg.agent, msg.prompt),
			a.tickCmd(),
		)
	case runCompletedMsg:
		a.kanban, _ = a.kanban.Reload()
		a.refreshActivity()
		if a.activeTicket != nil && a.activeTicket.ID == msg.ticketID {
			updated, err := a.orchestrator.GetTicket(context.Background(), msg.ticketID)
			if err == nil {
				a.activeTicket = &updated
				a.ticketView = a.ticketView.SetTicket(&updated)
			}
		}
		what := msg.ticketID
		if what == "" {
			what = "ad-hoc session " + msg.sessionID
		}
		return a, tea.Batch(
			a.showNotification("Run completed", fmt.Sprintf("Agent finished working on %s", what), NotificationSuccess),
			a.waitForCompletionCmd(),
		)
	case ticketMovedMsg:
		a.kanban, _ = a.kanban.Reload()
		where := "this board"
		if msg.repo != "" {
			where = filepath.Base(msg.repo)
		}
		return a, a.showNotification("Ticket moved", fmt.Sprintf("%s is now %s in %s", msg.from, msg.to, where), NotificationSuccess)
	case boardSyncMsg:
		if a.kanban.searchQuery == "" {
			a.kanban, _ = a.kanban.Reload()
			a.refreshActivity()
		}
		return a, boardSyncCmd()
	case agentActiveChangedMsg:
		// Emitted by the start*Cmd goroutines right after they call
		// StartApprovedRun/StartAdHocRun, which sets AgentActive=true in the
		// store. Reload the kanban so the activity bar starts animating, and
		// arm the tick.
		a.kanban, _ = a.kanban.Reload()
		a.refreshActivity()
		return a, a.tickCmd()
	case runStartFailedMsg:
		return a, a.showNotification("Run failed", msg.err.Error(), NotificationError)
	case proposalFailedMsg:
		delete(a.generatingProposals, msg.ticketID)
		if a.activeTicket != nil && a.activeTicket.ID == msg.ticketID {
			a.ticketView = a.ticketView.SetLoading(false)
		}
		return a, a.showNotification("Proposal failed", msg.err.Error(), NotificationError)
	case showDeleteModalMsg:
		ticketID := msg.ticketID
		a.modal.Open(
			"Delete Ticket",
			fmt.Sprintf("Delete %s?", ticketID),
			func() tea.Cmd {
				return func() tea.Msg {
					return deleteTicketConfirmMsg{ticketID: ticketID}
				}
			},
			nil,
		)
		return a, nil
	case deleteTicketConfirmMsg:
		if err := a.orchestrator.DeleteTicket(context.Background(), msg.ticketID); err != nil {
			return a, a.showNotification("Delete refused", err.Error(), NotificationError)
		}
		a.kanban, _ = a.kanban.Reload()
		return a, nil
	case mergeTicketRequestMsg:
		return a.handleMergeRequest(msg.ticketID)
	case mergeConfirmMsg:
		return a, a.mergeTicketCmd(msg.ticketID, msg.strategy)
	case mergeResultMsg:
		return a.handleMergeResult(msg)
	case deleteTicketRequestMsg:
		ticketID := msg.ticketID
		a.modal.Open(
			"Delete Ticket",
			fmt.Sprintf("Delete %s?", ticketID),
			func() tea.Cmd {
				return func() tea.Msg {
					return deleteTicketConfirmMsg{ticketID: ticketID}
				}
			},
			nil,
		)
		return a, nil
	case activity.Alert:
		// Longer than the usual toast: this one is a request for the user to
		// go do something, not a report that something happened. It has to
		// survive them looking away from the board.
		variant := NotificationWarning
		if msg.State == activity.StateBlocked {
			variant = NotificationError
		}
		a.refreshActivity()
		if a.waiting.Active() {
			a.reopenWaiting()
		}
		next := a.waitForActivityCmd()
		if msg.Decidable && a.resolvePermission != nil && !a.modal.Active() {
			a.openPermissionModal(msg)
			return a, next
		}
		return a, tea.Batch(
			a.notification.Show(msg.Title, msg.Body, variant, activityNotificationDuration),
			next,
		)

	case waitingResolveMsg:
		if a.resolvePermission != nil {
			a.resolvePermission(msg.sessionID, msg.decision)
		}
		a.refreshActivity()
		a.reopenWaiting()
		return a, a.tickCmd()
	case waitingJumpMsg:
		if err := a.orchestrator.SwitchToPane(msg.sessionID); err != nil {
			return a, a.showNotification("Cannot switch pane", err.Error(), NotificationError)
		}
		return a, a.tickCmd()
	case waitingClosedMsg:
		return a, a.tickCmd()
	case notificationMsg:
		return a, a.showNotification(msg.title, msg.message, msg.variant)
	case searchQueryMsg:
		results, err := a.orchestrator.ListTickets(context.Background(), store.TicketFilters{Search: msg.query})
		if err == nil {
			a.kanban, _ = a.kanban.Update(searchResultsMsg{tickets: results})
		}
		return a, nil
	case monthNavigateMsg:
		m := a.kanban
		m, _ = m.Update(msg)
		a.kanban = m
		return a, nil
	case tabChangeMsg:
		m := a.kanban
		m, _ = m.Update(msg)
		a.kanban = m
		return a, nil
	}

	return a, a.tickCmd()
}

func (a *App) tickCmd() tea.Cmd {
	return a.kanban.armTick()
}

func (a *App) loadProposalCmd(ticketID string) tea.Cmd {
	return func() tea.Msg {
		p, err := a.orchestrator.GetActiveProposalForTicket(context.Background(), ticketID)
		if err != nil {
			return proposalLoadedMsg{proposal: nil, TicketID: ticketID}
		}
		return proposalLoadedMsg{proposal: &p, TicketID: ticketID}
	}
}

func (a *App) approveProposalCmd(proposalID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		err := a.orchestrator.ApproveProposal(ctx, proposalID)
		if err != nil {
			return notificationMsg{title: "Error", message: err.Error(), variant: NotificationError}
		}
		// Reload proposal
		p, _ := a.orchestrator.GetProposal(context.Background(), proposalID)
		return proposalLoadedMsg{proposal: &p, TicketID: p.TicketID}
	}
}

// The start commands only start: completion arrives through the board's one
// listener, so a run that finishes an hour later still reports under its own
// ticket.
func (a *App) startRunAndListenCmd(proposalID string) tea.Cmd {
	return func() tea.Msg {
		_, err := a.orchestrator.StartApprovedRun(context.Background(), proposalID)
		if err != nil {
			return runStartFailedMsg{err: err}
		}
		return agentActiveChangedMsg{}
	}
}

func (a *App) startAdHocRunAndListenCmd(agent, prompt string) tea.Cmd {
	return func() tea.Msg {
		_, err := a.orchestrator.StartAdHocRun(context.Background(), "", agent, prompt)
		if err != nil {
			return runStartFailedMsg{err: err}
		}
		return agentActiveChangedMsg{}
	}
}

func (a *App) startDirectRunAndListenCmd(ticketID, agent, prompt string) tea.Cmd {
	return func() tea.Msg {
		_, err := a.orchestrator.StartAdHocRun(context.Background(), ticketID, agent, prompt)
		if err != nil {
			return runStartFailedMsg{err: err}
		}
		return agentActiveChangedMsg{}
	}
}

func (a *App) handleMergeRequest(ticketID string) (tea.Model, tea.Cmd) {
	err := a.orchestrator.CompleteTicket(context.Background(), ticketID)
	if err != nil {
		var mergeReq *orchestrator.MergeRequiredError
		if errors.As(err, &mergeReq) {
			if mergeReq.PRURL != "" {
				return a, a.showNotification(
					"PR not merged yet",
					fmt.Sprintf("%s has an open PR: %s", ticketID, mergeReq.PRURL),
					NotificationInfo,
				)
			}
			a.mergeModal.Open(mergeReq.TicketID, mergeReq.Branch, mergeReq.Strategy)
			return a, nil
		}
		return a, a.showNotification("Error", err.Error(), NotificationError)
	}
	return a.afterStatusChange(ticketID, "done")
}

func (a *App) handleStatusChanged(msg statusChangedMsg) (tea.Model, tea.Cmd) {
	if err := a.orchestrator.MoveTicketStatus(context.Background(), msg.ticketID, msg.newStatus); err != nil {
		return a, a.showNotification("Error", err.Error(), NotificationError)
	}
	return a.afterStatusChange(msg.ticketID, msg.newStatus)
}

func (a *App) afterStatusChange(ticketID, newStatus string) (tea.Model, tea.Cmd) {
	if a.activeTicket != nil && a.activeTicket.ID == ticketID {
		a.activeTicket.Status = newStatus
		a.ticketView = a.ticketView.SetTicket(a.activeTicket).SetLoading(false)
	}
	return a, a.showNotification("Status updated", fmt.Sprintf("%s moved to %s", ticketID, newStatus), NotificationSuccess)
}

func (a *App) mergeTicketCmd(ticketID string, strategy orchestrator.MergeStrategy) tea.Cmd {
	return func() tea.Msg {
		res, err := a.orchestrator.MergeTicket(context.Background(), ticketID, strategy)
		return mergeResultMsg{ticketID: ticketID, result: res, err: err}
	}
}

func (a *App) handleMergeResult(msg mergeResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return a, a.showNotification("Merge failed", msg.err.Error(), NotificationError)
	}

	switch msg.result.Outcome {
	case orchestrator.MergeOutcomeMerged:
		a.kanban, _ = a.kanban.Reload()
		if a.activeTicket != nil && a.activeTicket.ID == msg.ticketID {
			if updated, err := a.orchestrator.GetTicket(context.Background(), msg.ticketID); err == nil {
				a.activeTicket = &updated
				a.ticketView = a.ticketView.SetTicket(&updated)
			}
		}
		return a, a.showNotification(
			"Merged",
			fmt.Sprintf("%s merged into main, worktree cleaned up", msg.ticketID),
			NotificationSuccess,
		)
	case orchestrator.MergeOutcomePRCreated:
		a.kanban, _ = a.kanban.Reload()
		return a, a.showNotification(
			"PR created",
			fmt.Sprintf("%s: %s", msg.ticketID, msg.result.PRURL),
			NotificationSuccess,
		)
	case orchestrator.MergeOutcomeConflict:
		return a, a.showNotification(
			"Merge conflict",
			fmt.Sprintf("%s stays in review: %s", msg.ticketID, msg.result.Detail),
			NotificationError,
		)
	case orchestrator.MergeOutcomeDirty:
		return a, a.showNotification(
			"Cannot merge",
			msg.result.Detail,
			NotificationError,
		)
	default:
		return a, a.showNotification("Merge", msg.result.Detail, NotificationInfo)
	}
}

func (a *App) handleGenerateProposal(msg generateProposalMsg) (tea.Model, tea.Cmd) {
	if a.activeTicket == nil || a.activeTicket.ID != msg.ticketID {
		return a, nil
	}

	a.ticketView = a.ticketView.SetLoading(true)
	a.generatingProposals[msg.ticketID] = true

	return a, a.createProposalCmd(msg.ticketID)
}

func (a *App) createProposalCmd(ticketID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		debugLog.Printf("createProposal: ticket=%s", ticketID)
		proposal, err := a.orchestrator.CreateProposal(ctx, orchestrator.CreateProposalInput{
			TicketID: ticketID,
		})
		if err != nil {
			debugLog.Printf("createProposal FAILED: %v", err)
			return proposalFailedMsg{ticketID: ticketID, err: err}
		}
		debugLog.Printf("createProposal OK: id=%s", proposal.ID)
		return proposalCreatedMsg{proposal: proposal}
	}
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.waiting.Active() {
		var cmd tea.Cmd
		a.waiting, cmd = a.waiting.Update(msg)
		return a, cmd
	}
	if a.mergeModal.Active() {
		var cmd tea.Cmd
		a.mergeModal, cmd = a.mergeModal.Update(msg)
		return a, cmd
	}
	if a.modal.Active() {
		var cmd tea.Cmd
		a.modal, cmd = a.modal.Update(msg)
		return a, cmd
	}

	if a.textInput.Active() {
		var cmd tea.Cmd
		a.textInput, cmd = a.textInput.Update(msg)
		return a, cmd
	}

	if a.palette.Active() {
		a.palette, _ = a.palette.Update(msg)
		if !a.palette.Active() {
			if a.quit {
				return a, tea.Quit
			}
			if a.runCommand != nil {
				cmd := a.runCommand
				a.runCommand = nil
				return a, cmd
			}
		}
		return a, nil
	}

	key := msg.String()
	action, _ := a.resolver.Resolve(key)

	if a.view == viewBoard && a.kanban.IsSearchActive() {
		if action != keybinding.ActionNextTab && action != keybinding.ActionPrevTab && action != keybinding.ActionForceQuit {
			var cmd tea.Cmd
			a.kanban, cmd = a.kanban.Update(msg)
			return a, cmd
		}
	}

	if key == "esc" {
		if a.view == viewTicket && (a.ticketView.mode == ticketEditMode || a.ticketView.mode == ticketAgentSelectMode) {
			var cmd tea.Cmd
			a.ticketView, cmd = a.ticketView.Update(msg)
			return a, cmd
		}
		if a.view == viewTicket {
			a.kanban, _ = a.kanban.Reload()
		}
		if a.view != viewBoard {
			a.view = viewBoard
			a.activeTicket = nil
			return a, a.tickCmd()
		}
	}

	if a.view == viewTicket && (a.ticketView.mode == ticketEditMode || a.ticketView.mode == ticketAdHocPromptMode) {
		var cmd tea.Cmd
		a.ticketView, cmd = a.ticketView.Update(msg)
		return a, cmd
	}

	if a.view == viewTicket && action != keybinding.ActionShowDashboard {
		var cmd tea.Cmd
		a.ticketView, cmd = a.ticketView.Update(msg)
		return a, cmd
	}

	if a.view == viewDashboard && action != keybinding.ActionShowDashboard {
		a.dashboard, _ = a.dashboard.Update(msg)
		return a, nil
	}

	switch action {
	case keybinding.ActionQuit:
		a.modal.Open(
			"Quit AgentBoard",
			"Are you sure you want to quit?",
			func() tea.Cmd { return tea.Quit },
			nil,
		)
	case keybinding.ActionForceQuit:
		return a, tea.Quit
	case keybinding.ActionOpenTicket:
		selected := a.kanban.SelectedTicket()
		if selected != nil {
			a.activeTicket = selected
			a.ticketView = a.ticketView.SetTicket(selected)
			a.view = viewTicket
			a.ticketView = a.ticketView.SetLoading(a.generatingProposals[selected.ID])
			p, _ := a.orchestrator.GetActiveProposalForTicket(context.Background(), selected.ID)
			if p.ID != "" {
				a.ticketView = a.ticketView.SetProposal(&p)
			}
			return a, nil
		}
	case keybinding.ActionShowHelp:
		if a.view == viewHelp {
			a.view = viewBoard
			return a, a.tickCmd()
		}
		a.view = viewHelp
		return a, nil
	case keybinding.ActionShowDashboard:
		if a.view == viewDashboard {
			a.view = viewBoard
			return a, a.tickCmd()
		}
		a.dashboard = a.dashboard.Refresh()
		a.view = viewDashboard
		return a, nil
	case keybinding.ActionOpenPalette:
		a.palette.Open()
	case keybinding.ActionShowWaiting:
		a.refreshActivity()
		a.waiting.Open(a.waitingItems())
		return a, nil
	case keybinding.ActionRefresh:
		newCfg, err := config.LoadFromDir(config.GetBaseDir(), a.config.Project())
		if err == nil {
			a.config = newCfg
			a.orchestrator.SetTicketPrefix(newCfg.Board.Prefix)
			a.kanban, _ = a.kanban.UpdateColumnDefs(newCfg.Board.Columns)
		} else {
			a.kanban, _ = a.kanban.Reload()
		}
	default:
		if index, ok := jumpSessionActions[action]; ok {
			a.view = viewDashboard
			a.dashboard = a.dashboard.Refresh()
			a.dashboard.JumpToSession(index)
			return a, nil
		}
		var cmd tea.Cmd
		a.kanban, cmd = a.kanban.Update(msg)
		return a, cmd
	}

	return a, nil
}

func (a *App) showNotification(title, message string, variant NotificationVariant) tea.Cmd {
	dur := 2 * time.Second
	if variant == NotificationError {
		dur = 5 * time.Second
	}
	return a.notification.Show(title, message, variant, dur)
}

func (a *App) renderHeader() string {
	t := a.registry.Active()
	muted := lipgloss.Color("240")
	if t != nil {
		muted = t.TextMuted
	}
	fg := lipgloss.Color("252")
	if t != nil {
		fg = t.Text
	}
	primary := lipgloss.Color("69")
	if t != nil {
		primary = t.Primary
	}

	name := a.config.ProjectName
	if name == "" {
		name = "AgentBoard"
	}
	labelStyle := lipgloss.NewStyle().Foreground(primary)
	nameStyle := lipgloss.NewStyle().Bold(true).Foreground(fg)
	hintStyle := lipgloss.NewStyle().Foreground(muted)
	km := a.resolver.KeyMap()

	hintPart := hintStyle.Render(km.BindingsForActions(keybinding.ActionAddTicket, keybinding.ActionDeleteTicket))
	projectPart := labelStyle.Render("  Project: ") + nameStyle.Render(name)
	if a.waitingCount > 0 {
		warn := lipgloss.Color("214")
		if t != nil && t.Warning != "" {
			warn = t.Warning
		}
		waitStyle := lipgloss.NewStyle().Bold(true).Foreground(warn)
		projectPart += waitStyle.Render(fmt.Sprintf("   %d waiting", a.waitingCount)) +
			hintStyle.Render(" ("+km.GetByAction(keybinding.ActionShowWaiting)+")")
	}
	hintPart2 := hintStyle.Render(km.BindingsForActions(keybinding.ActionShowHelp, keybinding.ActionRefresh))

	projectWidth := lipgloss.Width(projectPart)
	hintWidth := lipgloss.Width(hintPart) + lipgloss.Width(hintPart2)
	available := a.width - hintWidth - 1
	if available < projectWidth {
		available = projectWidth
	}

	leftPad := (available - projectWidth) / 2
	if leftPad < 0 {
		leftPad = 0
	}

	left := hintPart + strings.Repeat(" ", leftPad) + projectPart
	right := hintPart2

	totalLeft := lipgloss.Width(left)
	gap := a.width - totalLeft - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + right
}

func (a *App) View() string {
	var mainView string
	switch a.view {
	case viewHelp:
		mainView = a.renderHelp()
	case viewTicket:
		mainView = a.ticketView.View()
	case viewDashboard:
		mainView = a.dashboard.View()
	default:
		mainView = a.kanban.View()
	}
	header := a.renderHeader()
	t := a.registry.Active()
	separator := lipgloss.NewStyle().Foreground(t.Border).Render(strings.Repeat("─", a.width))

	mainViewHeight := a.height - 2
	if mainViewHeight < 0 {
		mainViewHeight = 0
	}
	mainView = lipgloss.NewStyle().Height(mainViewHeight).Render(mainView)
	mainView = header + "\n" + separator + "\n" + mainView

	if a.palette.Active() {
		paletteView := a.palette.View()
		paletteLines := strings.Split(paletteView, "\n")
		paletteHeight := len(paletteLines)
		bgLines := strings.Split(mainView, "\n")

		for len(bgLines) < a.height {
			bgLines = append(bgLines, "")
		}

		startY := a.height - paletteHeight
		var finalView strings.Builder
		for i := 0; i < a.height; i++ {
			bgLine := ""
			if i < len(bgLines) {
				bgLine = bgLines[i]
			}

			if i >= startY && i < a.height {
				row := i - startY
				paletteLine := paletteLines[row]
				// Palette is docked at the left (x=0)
				finalView.WriteString(overlayLine(bgLine, paletteLine, 0))
			} else {
				finalView.WriteString(bgLine)
			}
			if i < a.height-1 {
				finalView.WriteString("\n")
			}
		}
		mainView = finalView.String()
	}

	if a.waiting.Active() {
		return overlayCentered(mainView, a.waiting.ViewBox(), a.width, a.height)
	}

	if a.mergeModal.Active() {
		modalBox := a.mergeModal.ViewBox()
		modalHeight := lipgloss.Height(modalBox)
		modalWidth := lipgloss.Width(modalBox)
		bgLines := strings.Split(mainView, "\n")

		for len(bgLines) < a.height {
			bgLines = append(bgLines, "")
		}

		startY := (a.height - modalHeight) / 2
		startX := (a.width - modalWidth) / 2

		var finalView strings.Builder
		modalLines := strings.Split(modalBox, "\n")

		for i := 0; i < a.height; i++ {
			bgLine := ""
			if i < len(bgLines) {
				bgLine = bgLines[i]
			}

			if i >= startY && i < startY+modalHeight {
				row := i - startY
				modalLine := modalLines[row]
				finalView.WriteString(overlayLine(bgLine, modalLine, startX))
			} else {
				finalView.WriteString(bgLine)
			}
			if i < a.height-1 {
				finalView.WriteString("\n")
			}
		}
		return finalView.String()
	}

	if a.modal.Active() {
		modalBox := a.modal.ViewBox()
		modalHeight := lipgloss.Height(modalBox)
		modalWidth := lipgloss.Width(modalBox)
		bgLines := strings.Split(mainView, "\n")

		for len(bgLines) < a.height {
			bgLines = append(bgLines, "")
		}

		startY := (a.height - modalHeight) / 2
		startX := (a.width - modalWidth) / 2

		var finalView strings.Builder
		modalLines := strings.Split(modalBox, "\n")

		for i := 0; i < a.height; i++ {
			bgLine := ""
			if i < len(bgLines) {
				bgLine = bgLines[i]
			}

			if i >= startY && i < startY+modalHeight {
				row := i - startY
				modalLine := modalLines[row]
				finalView.WriteString(overlayLine(bgLine, modalLine, startX))
			} else {
				finalView.WriteString(bgLine)
			}
			if i < a.height-1 {
				finalView.WriteString("\n")
			}
		}
		return finalView.String()
	}

	if a.textInput.Active() {
		return a.textInput.View()
	}

	if a.notification.Active() {
		notificationStack := a.notification.View()
		notificationHeight := lipgloss.Height(notificationStack)
		notificationWidth := lipgloss.Width(notificationStack)
		bgLines := strings.Split(mainView, "\n")

		for len(bgLines) < a.height {
			bgLines = append(bgLines, "")
		}

		startY := a.height - notificationHeight - 1
		if startY < 1 {
			startY = 1
		}
		startX := a.width - notificationWidth - 2
		if startX < 0 {
			startX = 0
		}

		var finalView strings.Builder
		notificationLines := strings.Split(notificationStack, "\n")

		for i := 0; i < a.height; i++ {
			bgLine := ""
			if i < len(bgLines) {
				bgLine = bgLines[i]
			}

			if i >= startY && i < startY+notificationHeight {
				row := i - startY
				notificationLine := notificationLines[row]
				finalView.WriteString(overlayLine(bgLine, notificationLine, startX))
			} else {
				finalView.WriteString(bgLine)
			}
			if i < a.height-1 {
				finalView.WriteString("\n")
			}
		}
		return finalView.String()
	}

	return mainView
}

// overlayLine places fg over bg at the given x offset, preserving bg on both sides.
func overlayLine(bg, fg string, x int) string {
	bgWidth := lipgloss.Width(bg)
	fgWidth := lipgloss.Width(fg)

	if x < 0 {
		x = 0
	}
	if x+fgWidth > bgWidth {
		x = (bgWidth - fgWidth) / 2
	}

	left := ansiTruncate(bg, x)
	right := ansiSkip(bg, x+fgWidth)

	return left + fg + right
}

// ansiTruncate returns the part of the string up to the given visual width.
func ansiTruncate(s string, limit int) string {
	var (
		visualPos int
		inEscape  bool
	)

	for i, r := range s {
		if visualPos >= limit && !inEscape {
			return s[:i]
		}

		if r == '\x1b' {
			inEscape = true
			continue
		}

		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}

		visualPos++
	}
	return s
}

// ansiSkip returns the part of the string starting after the given visual width.
func ansiSkip(s string, skip int) string {
	var (
		visualPos int
		inEscape  bool
	)

	for i, r := range s {
		if visualPos >= skip && !inEscape {
			return s[i:]
		}

		if r == '\x1b' {
			inEscape = true
			continue
		}

		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}

		// Simplified width check (most TUI chars are width 1)
		visualPos++
	}
	return ""
}

func (a *App) renderHelp() string {
	t := a.registry.Active()
	primary := lipgloss.Color("69")
	if t != nil {
		primary = t.Primary
	}

	var b strings.Builder
	helpTitle := lipgloss.NewStyle().Bold(true).Foreground(primary).Render("Help — Keybindings")
	fmt.Fprintf(&b, "%s\n\n", helpTitle)
	km := keybinding.DefaultKeyMap()
	for _, binding := range km.Bindings {
		fmt.Fprintf(&b, "  %-12s %s\n", binding.Key, binding.Action.String())
	}
	fmt.Fprint(&b, "\nPress ? to return")
	return b.String()
}

// overlayCentered draws box over the middle of background, the same way the
// modals are composited.
func overlayCentered(background, box string, width, height int) string {
	boxHeight := lipgloss.Height(box)
	boxWidth := lipgloss.Width(box)
	bgLines := strings.Split(background, "\n")
	for len(bgLines) < height {
		bgLines = append(bgLines, "")
	}
	startY := (height - boxHeight) / 2
	startX := (width - boxWidth) / 2
	if startY < 0 {
		startY = 0
	}
	if startX < 0 {
		startX = 0
	}
	boxLines := strings.Split(box, "\n")

	var out strings.Builder
	for i := 0; i < height; i++ {
		bgLine := ""
		if i < len(bgLines) {
			bgLine = bgLines[i]
		}
		if i >= startY && i < startY+boxHeight {
			out.WriteString(overlayLine(bgLine, boxLines[i-startY], startX))
		} else {
			out.WriteString(bgLine)
		}
		if i < height-1 {
			out.WriteString("\n")
		}
	}
	return out.String()
}
