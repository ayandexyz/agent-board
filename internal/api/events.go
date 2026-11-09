package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ayandexyz/agent-board/internal/apitypes"
)

// eventBuffer is how many past events a reconnecting client can still
// replay with ?after=. Older than that it must refetch the board.
const eventBuffer = 256

const heartbeat = 15 * time.Second

type eventHub struct {
	mu     sync.Mutex
	seq    uint64
	ring   []apitypes.Event
	size   int
	subs   map[chan apitypes.Event]struct{}
	closed bool
}

func newEventHub(size int) *eventHub {
	return &eventHub{size: size, subs: map[chan apitypes.Event]struct{}{}}
}

func (h *eventHub) publish(kind string, data interface{}) apitypes.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	e := apitypes.Event{Seq: h.seq, Type: kind, At: time.Now(), Data: data}
	h.ring = append(h.ring, e)
	if len(h.ring) > h.size {
		h.ring = h.ring[len(h.ring)-h.size:]
	}
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
		}
	}
	return e
}

// subscribe returns everything after seq that is still buffered, then a live
// channel. Both are computed under one lock so nothing falls in the gap.
func (h *eventHub) subscribe(after uint64) ([]apitypes.Event, chan apitypes.Event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var backlog []apitypes.Event
	for _, e := range h.ring {
		if e.Seq > after {
			backlog = append(backlog, e)
		}
	}
	ch := make(chan apitypes.Event, h.size)
	if h.closed {
		close(ch)
		return backlog, ch, func() {}
	}
	h.subs[ch] = struct{}{}
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			h.mu.Unlock()
		})
	}
	return backlog, ch, cancel
}

func (h *eventHub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for ch := range h.subs {
		delete(h.subs, ch)
		close(ch)
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	var after uint64
	if v := r.URL.Query().Get("after"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "after must be a number")
			return
		}
		after = n
	}
	if v := r.Header.Get("Last-Event-ID"); v != "" && after == 0 {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			after = n
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	backlog, live, cancel := s.hub.subscribe(after)
	defer cancel()
	for _, e := range backlog {
		if err := writeEvent(w, e); err != nil {
			return
		}
	}
	flusher.Flush()

	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case e, ok := <-live:
			if !ok {
				return
			}
			if err := writeEvent(w, e); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeEvent(w http.ResponseWriter, e apitypes.Event) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Type, data)
	return err
}
