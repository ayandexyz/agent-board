package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
)

// Terminal is a live screen the API can stream: bytes out, keystrokes in,
// and a resize when the viewer's window changes.
type Terminal interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
}

// TerminalOpener attaches a viewer to the board's tmux session. windowID
// is empty for the whole session and a tmux window id ("@3") to land on one
// agent's window. Nil on the server means the route answers 501.
type TerminalOpener interface {
	Open(windowID string, cols, rows int) (Terminal, error)
}

// control is the one text frame a client may send; every binary frame is
// raw input for the terminal.
type control struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

const (
	defaultCols  = 80
	defaultRows  = 24
	readChunk    = 32 * 1024
	writeTimeout = 10 * time.Second
)

func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	s.serveTerminal(w, r, "")
}

func (s *Server) handleSessionTerminal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var windowID string
	for _, as := range s.Board.GetActiveSessions() {
		if as.SessionID == id {
			windowID = as.WindowID
			break
		}
	}
	if windowID == "" {
		writeError(w, http.StatusNotFound, "no active session with that id")
		return
	}
	s.serveTerminal(w, r, windowID)
}

func (s *Server) serveTerminal(w http.ResponseWriter, r *http.Request, windowID string) {
	if s.Terminals == nil {
		writeError(w, http.StatusNotImplemented, "no terminal backend is configured")
		return
	}
	cols, rows := sizeFromQuery(r)
	term, err := s.Terminals.Open(windowID, cols, rows)
	if err != nil {
		writeErr(w, err)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		_ = term.Close()
		return
	}
	defer term.Close()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		defer cancel()
		buf := make([]byte, readChunk)
		for {
			n, err := term.Read(buf)
			if n > 0 {
				wctx, wcancel := context.WithTimeout(ctx, writeTimeout)
				werr := conn.Write(wctx, websocket.MessageBinary, buf[:n])
				wcancel()
				if werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		switch typ {
		case websocket.MessageBinary:
			if _, err := term.Write(data); err != nil {
				conn.Close(websocket.StatusInternalError, "terminal write failed")
				return
			}
		case websocket.MessageText:
			var c control
			if json.Unmarshal(data, &c) == nil && c.Type == "resize" {
				_ = term.Resize(c.Cols, c.Rows)
			}
		}
	}
	conn.Close(websocket.StatusNormalClosure, "")
}

func sizeFromQuery(r *http.Request) (cols, rows int) {
	cols, rows = defaultCols, defaultRows
	if v, err := strconv.Atoi(r.URL.Query().Get("cols")); err == nil && v > 0 {
		cols = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("rows")); err == nil && v > 0 {
		rows = v
	}
	return cols, rows
}
