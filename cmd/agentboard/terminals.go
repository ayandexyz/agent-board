package main

import (
	"github.com/ayandexyz/agent-board/internal/api"
	"github.com/ayandexyz/agent-board/internal/tmux"
)

// tmuxTerminals gives the API a viewer on the board's own tmux session.
type tmuxTerminals struct {
	session string
}

func (t tmuxTerminals) Open(windowID string, cols, rows int) (api.Terminal, error) {
	a, err := tmux.Attach(t.session, windowID, cols, rows)
	if err != nil {
		return nil, err
	}
	return a, nil
}
