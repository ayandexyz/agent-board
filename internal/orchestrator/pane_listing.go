package orchestrator

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// paneListingMaxAge is how long one `tmux list-panes -a` answers every
// monitor. Just under the monitors' 500ms poll, so each poll round shares a
// single listing however many agents are running.
const paneListingMaxAge = 400 * time.Millisecond

type paneStatus struct {
	dead    bool
	command string
}

type paneListing struct {
	mu    sync.Mutex
	at    time.Time
	panes map[string]paneStatus
}

// status reports one pane from a listing shared by every monitor. A pane
// missing from a cached listing triggers a fresh one before it is reported
// gone, so a window created after the last listing is never mistaken for a
// closed one.
func (l *paneListing) status(paneID string) (paneStatus, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	fresh := false
	if l.panes == nil || time.Since(l.at) > paneListingMaxAge {
		if err := l.refresh(); err != nil {
			return paneStatus{}, false, err
		}
		fresh = true
	}
	st, ok := l.panes[paneID]
	if !ok && !fresh {
		if err := l.refresh(); err != nil {
			return paneStatus{}, false, err
		}
		st, ok = l.panes[paneID]
	}
	return st, ok, nil
}

func (l *paneListing) refresh() error {
	out, err := exec.Command("tmux", "list-panes", "-a", "-F",
		"#{pane_id}:#{pane_pid}:#{pane_dead}:#{pane_current_command}").Output()
	if err != nil {
		return fmt.Errorf("orchestrator.listPanes: %w", err)
	}
	panes := map[string]paneStatus{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.SplitN(line, ":", 4)
		if len(fields) < 4 {
			continue
		}
		panes[fields[0]] = paneStatus{dead: fields[2] == "1", command: fields[3]}
	}
	l.panes, l.at = panes, time.Now()
	return nil
}
