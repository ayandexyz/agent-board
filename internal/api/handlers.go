package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ayandexyz/agent-board/internal/activity"
	"github.com/ayandexyz/agent-board/internal/apitypes"
	"github.com/ayandexyz/agent-board/internal/orchestrator"
	"github.com/ayandexyz/agent-board/internal/store"
)

const maxBody = 1 << 20

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /api/v1/identity", s.handleIdentity)
	m.HandleFunc("GET /api/v1/board", s.handleBoard)
	m.HandleFunc("GET /api/v1/events", s.handleEvents)

	m.HandleFunc("GET /api/v1/tickets", s.handleListTickets)
	m.HandleFunc("POST /api/v1/tickets", s.handleCreateTicket)
	m.HandleFunc("GET /api/v1/tickets/{id}", s.handleGetTicket)
	m.HandleFunc("PATCH /api/v1/tickets/{id}", s.handleUpdateTicket)
	m.HandleFunc("DELETE /api/v1/tickets/{id}", s.handleDeleteTicket)
	m.HandleFunc("POST /api/v1/tickets/{id}/status", s.handleSetStatus)
	m.HandleFunc("POST /api/v1/tickets/{id}/branch-mode", s.handleSetBranchMode)
	m.HandleFunc("POST /api/v1/tickets/{id}/start", s.handleStart)
	m.HandleFunc("POST /api/v1/tickets/{id}/complete", s.handleComplete)
	m.HandleFunc("POST /api/v1/tickets/{id}/merge", s.handleMerge)
	m.HandleFunc("GET /api/v1/tickets/{id}/diff", s.handleDiff)

	m.HandleFunc("GET /api/v1/terminal", s.handleTerminal)
	m.HandleFunc("GET /api/v1/sessions", s.handleListSessions)
	m.HandleFunc("GET /api/v1/sessions/{id}/terminal", s.handleSessionTerminal)
	m.HandleFunc("GET /api/v1/sessions/{id}/screen", s.handleScreen)
	m.HandleFunc("POST /api/v1/sessions/{id}/stop", s.handleStop)
	m.HandleFunc("POST /api/v1/sessions/{id}/input", s.handleInput)
	m.HandleFunc("POST /api/v1/sessions/{id}/resolve", s.handleResolve)
}

func (s *Server) handleIdentity(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, apitypes.Identity{
		Name:        s.Identity.Name,
		Version:     s.Identity.Version,
		Project:     s.Identity.Project,
		TmuxSession: s.Identity.TmuxSession,
		API:         "v1",
	})
}

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	tickets, err := s.Board.ListTickets(r.Context(), store.TicketFilters{})
	if err != nil {
		writeErr(w, err)
		return
	}
	sessions := s.sessionsByTicket()
	byStatus := map[string][]apitypes.Ticket{}
	for _, t := range tickets {
		byStatus[t.Status] = append(byStatus[t.Status], s.ticketDTO(t, sessions))
	}
	board := apitypes.Board{Waiting: s.waitingCount()}
	for _, status := range s.Board.Statuses() {
		col := apitypes.Column{Status: status, Tickets: byStatus[status]}
		if col.Tickets == nil {
			col.Tickets = []apitypes.Ticket{}
		}
		board.Columns = append(board.Columns, col)
	}
	if board.Columns == nil {
		board.Columns = []apitypes.Column{}
	}
	writeJSON(w, http.StatusOK, board)
}

func (s *Server) handleListTickets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tickets, err := s.Board.ListTickets(r.Context(), store.TicketFilters{
		Status:   q.Get("status"),
		Agent:    q.Get("agent"),
		Priority: q.Get("priority"),
		Tag:      q.Get("tag"),
		Search:   q.Get("search"),
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	sessions := s.sessionsByTicket()
	out := make([]apitypes.Ticket, 0, len(tickets))
	for _, t := range tickets {
		out = append(out, s.ticketDTO(t, sessions))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateTicket(w http.ResponseWriter, r *http.Request) {
	var in apitypes.CreateTicket
	if !decode(w, r, &in) {
		return
	}
	t, err := s.Board.CreateTicket(r.Context(), orchestrator.CreateTicketInput{
		Title:    in.Title,
		Prompt:   in.Prompt,
		Priority: in.Priority,
		Agent:    in.Agent,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	s.hub.publish("ticket.created", map[string]string{"ticket_id": t.ID})
	writeJSON(w, http.StatusCreated, s.ticketDTO(t, nil))
}

func (s *Server) handleGetTicket(w http.ResponseWriter, r *http.Request) {
	t, err := s.Board.GetTicket(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.ticketDTO(t, s.sessionsByTicket()))
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, err := s.Board.TicketDiff(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := apitypes.TicketDiff{TicketID: id, Base: d.Base, Patch: d.Patch, Files: []apitypes.FileChange{}}
	for _, f := range d.Files {
		out.Added += f.Added
		out.Deleted += f.Deleted
		out.Files = append(out.Files, apitypes.FileChange{
			Path: f.Path, Added: f.Added, Deleted: f.Deleted, Binary: f.Binary, Untracked: f.Untracked,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUpdateTicket(w http.ResponseWriter, r *http.Request) {
	var in apitypes.UpdateTicket
	if !decode(w, r, &in) {
		return
	}
	t, err := s.Board.GetTicket(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if in.Title != nil {
		t.Title = *in.Title
	}
	if in.Prompt != nil {
		t.Prompt = *in.Prompt
	}
	if in.Priority != nil {
		t.Priority = *in.Priority
	}
	if in.Agent != nil {
		t.Agent = *in.Agent
	}
	if in.Branch != nil {
		t.Branch = *in.Branch
	}
	if in.Tags != nil {
		t.Tags = *in.Tags
	}
	if in.DependsOn != nil {
		t.DependsOn = *in.DependsOn
	}
	updated, err := s.Board.UpdateTicket(r.Context(), t)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.hub.publish("ticket.changed", map[string]string{"ticket_id": updated.ID})
	writeJSON(w, http.StatusOK, s.ticketDTO(updated, s.sessionsByTicket()))
}

func (s *Server) handleDeleteTicket(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Board.DeleteTicket(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	s.hub.publish("ticket.deleted", map[string]string{"ticket_id": id})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSetStatus(w http.ResponseWriter, r *http.Request) {
	var in apitypes.SetStatus
	if !decode(w, r, &in) {
		return
	}
	id := r.PathValue("id")
	if err := s.Board.MoveTicketStatus(r.Context(), id, in.Status); err != nil {
		writeErr(w, err)
		return
	}
	s.hub.publish("ticket.changed", map[string]string{"ticket_id": id, "status": in.Status})
	s.respondTicket(w, r, id)
}

func (s *Server) handleSetBranchMode(w http.ResponseWriter, r *http.Request) {
	var in apitypes.SetBranchMode
	if !decode(w, r, &in) {
		return
	}
	t, err := s.Board.SetBranchMode(r.Context(), r.PathValue("id"), in.Mode)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.hub.publish("ticket.changed", map[string]string{"ticket_id": t.ID})
	writeJSON(w, http.StatusOK, s.ticketDTO(t, s.sessionsByTicket()))
}

// handleStart runs the ticket's own agent on the ticket's own prompt unless
// the body names another; the agent gets the prompt the user wrote and
// nothing else.
func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	var in apitypes.StartRun
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	t, err := s.Board.GetTicket(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	agent := in.Agent
	if agent == "" {
		agent = t.Agent
	}
	if agent == "" {
		writeError(w, http.StatusBadRequest, "ticket has no agent; pass one in the body")
		return
	}
	prompt := in.Prompt
	if prompt == "" {
		prompt = t.Prompt
	}
	sess, err := s.Board.StartAdHocRun(r.Context(), t.ID, agent, prompt)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.hub.publish("session.started", map[string]string{"ticket_id": t.ID, "session_id": sess.ID, "agent": agent})
	writeJSON(w, http.StatusCreated, apitypes.Session{
		ID:        sess.ID,
		TicketID:  sess.TicketID,
		Agent:     sess.Agent,
		Status:    sess.Status,
		StartedAt: sess.StartedAt,
		Window:    s.Board.WindowForSession(sess.ID),
	})
}

func (s *Server) handleComplete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.Board.CompleteTicket(r.Context(), id)
	var mr *orchestrator.MergeRequiredError
	if errors.As(err, &mr) {
		writeJSON(w, http.StatusConflict, apitypes.Error{
			Error: "merge required",
			MergeRequired: &apitypes.MergeRequired{
				TicketID: mr.TicketID,
				Branch:   mr.Branch,
				Strategy: string(mr.Strategy),
				PRURL:    mr.PRURL,
			},
		})
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	s.hub.publish("ticket.changed", map[string]string{"ticket_id": id, "status": "done"})
	s.respondTicket(w, r, id)
}

func (s *Server) handleMerge(w http.ResponseWriter, r *http.Request) {
	var in apitypes.Merge
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	id := r.PathValue("id")
	res, err := s.Board.MergeTicket(r.Context(), id, orchestrator.MergeStrategy(in.Strategy))
	if err != nil {
		writeErr(w, err)
		return
	}
	s.hub.publish("ticket.merged", map[string]string{"ticket_id": id, "outcome": string(res.Outcome)})
	writeJSON(w, http.StatusOK, apitypes.MergeResult{
		Outcome: string(res.Outcome),
		Branch:  res.Branch,
		PRURL:   res.PRURL,
		Detail:  res.Detail,
	})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	out := []apitypes.Session{}
	waiting := s.waitingByID()
	for _, as := range s.Board.GetActiveSessions() {
		dto := apitypes.Session{
			ID:        as.SessionID,
			TicketID:  as.TicketID,
			Agent:     as.Agent,
			Status:    as.Status,
			StartedAt: time.Unix(as.StartedAt, 0),
			Window:    s.Board.WindowForSession(as.SessionID),
			WindowID:  as.WindowID,
			PaneID:    as.PaneID,
		}
		if s.Activity != nil {
			dto.Activity = string(s.Activity.StateOf(as.SessionID))
		}
		if wsess, ok := waiting[as.SessionID]; ok {
			dto.ToolName = wsess.ToolName
			dto.Detail = wsess.Detail
			dto.Decidable = wsess.Decidable
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleScreen(w http.ResponseWriter, r *http.Request) {
	lines := 50
	if v := r.URL.Query().Get("lines"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "lines must be a positive number")
			return
		}
		lines = n
	}
	id := r.PathValue("id")
	content, err := s.Board.GetPaneContent(id, lines)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apitypes.Screen{SessionID: id, Lines: lines, Content: content})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Board.StopSession(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	s.hub.publish("session.stopped", map[string]string{"session_id": id})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleInput(w http.ResponseWriter, r *http.Request) {
	var in apitypes.Input
	if !decode(w, r, &in) {
		return
	}
	if err := s.Board.SendInput(r.PathValue("id"), in.Text); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	var in apitypes.Resolve
	if !decode(w, r, &in) {
		return
	}
	if in.Decision != "allow" && in.Decision != "deny" {
		writeError(w, http.StatusBadRequest, `decision must be "allow" or "deny"`)
		return
	}
	if s.Activity == nil {
		writeError(w, http.StatusConflict, "no activity receiver is running")
		return
	}
	id := r.PathValue("id")
	if !s.Activity.Resolve(id, in.Decision) {
		writeError(w, http.StatusNotFound, "no permission request is held for this session")
		return
	}
	s.hub.publish("permission.resolved", map[string]string{"session_id": id, "decision": in.Decision})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) respondTicket(w http.ResponseWriter, r *http.Request, id string) {
	t, err := s.Board.GetTicket(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.ticketDTO(t, s.sessionsByTicket()))
}

func (s *Server) sessionsByTicket() map[string]*orchestrator.AgentSession {
	out := map[string]*orchestrator.AgentSession{}
	for _, as := range s.Board.GetActiveSessions() {
		if as.TicketID != "" {
			out[as.TicketID] = as
		}
	}
	return out
}

func (s *Server) waitingByID() map[string]activity.Session {
	out := map[string]activity.Session{}
	if s.Activity == nil {
		return out
	}
	for _, w := range s.Activity.Waiting() {
		out[w.ID] = w
	}
	return out
}

func (s *Server) waitingCount() int {
	if s.Activity == nil {
		return 0
	}
	return len(s.Activity.Waiting())
}

func (s *Server) ticketDTO(t store.Ticket, sessions map[string]*orchestrator.AgentSession) apitypes.Ticket {
	dto := apitypes.Ticket{
		ID:            t.ID,
		Title:         t.Title,
		Prompt:        t.Prompt,
		Status:        t.Status,
		Priority:      t.Priority,
		Agent:         t.Agent,
		Branch:        t.Branch,
		BranchMode:    t.BranchMode,
		WorktreePath:  t.WorktreePath,
		PRURL:         t.PRURL,
		Tags:          t.Tags,
		DependsOn:     t.DependsOn,
		AgentActive:   t.AgentActive,
		ResumeCommand: t.ResumeCommand,
		CreatedAt:     t.CreatedAt,
		UpdatedAt:     t.UpdatedAt,
	}
	if dto.Tags == nil {
		dto.Tags = []string{}
	}
	if dto.DependsOn == nil {
		dto.DependsOn = []string{}
	}
	if as, ok := sessions[t.ID]; ok {
		dto.SessionID = as.SessionID
		if s.Activity != nil {
			dto.Activity = string(s.Activity.StateOf(as.SessionID))
		}
	}
	return dto
}

func decode(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "cannot read body")
		return false
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		writeError(w, http.StatusBadRequest, "body must be JSON")
		return false
	}
	if err := json.Unmarshal(body, v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apitypes.Error{Error: msg})
}

// writeErr maps the sentinels the container raises onto status codes so a
// client can branch on them without parsing messages.
func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrInvalidStatus),
		errors.Is(err, store.ErrInvalidPriority),
		errors.Is(err, orchestrator.ErrTitleRequired),
		errors.Is(err, orchestrator.ErrInvalidBranchMode),
		errors.Is(err, orchestrator.ErrUnknownAgent):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, orchestrator.ErrTicketBusy),
		errors.Is(err, orchestrator.ErrInputSuppressed),
		errors.Is(err, orchestrator.ErrNoRepository):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
