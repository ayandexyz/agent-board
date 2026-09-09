package activity

// eventBuffer is how many alerts one subscriber may fall behind by before it
// starts losing them. A hook request must never wait on a slow reader, so the
// choice is between dropping and blocking, and dropping is the safe one: a
// missed toast costs a notification, a blocked hook stalls the agent.
const eventBuffer = 16

// subscription is one consumer of the alert stream — the TUI, an API
// websocket, a phone's push bridge. Each gets its own buffer so a stalled one
// slows nobody else down.
type subscription struct {
	ch     chan Alert
	closed bool
}

// Subscribe returns a stream of "this session needs a human" alerts and a
// function that ends it. Every subscriber sees every alert raised after it
// subscribed; the channel is closed on cancel or on Receiver.Close, so a reader
// ranging over it terminates. Cancel is safe to call more than once.
func (r *Receiver) Subscribe() (<-chan Alert, func()) {
	sub := &subscription{ch: make(chan Alert, eventBuffer)}
	r.mu.Lock()
	r.subs[sub] = struct{}{}
	r.mu.Unlock()

	return sub.ch, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.subs, sub)
		sub.close()
	}
}

// close must be called with the receiver's lock held, which is what keeps a
// send in publish from racing it onto a closed channel.
func (s *subscription) close() {
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

// publish fans one alert out to every subscriber. The sends are non-blocking,
// so holding the lock across them costs nothing and closes the window in which
// a cancel could close a channel mid-send.
func (r *Receiver) publish(a Alert) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for sub := range r.subs {
		select {
		case sub.ch <- a:
		default:
		}
	}
}

// closeSubscriptions ends every stream, so consumers unblock when the receiver
// goes away rather than waiting on a channel nothing will ever write to again.
func (r *Receiver) closeSubscriptions() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for sub := range r.subs {
		sub.close()
		delete(r.subs, sub)
	}
}
