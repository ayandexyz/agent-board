package activity

import (
	"testing"
	"time"
)

func recvAlert(t *testing.T, ch <-chan Alert) Alert {
	t.Helper()
	select {
	case a, ok := <-ch:
		if !ok {
			t.Fatal("subscription closed")
		}
		return a
	case <-time.After(time.Second):
		t.Fatal("no alert delivered")
	}
	return Alert{}
}

func TestEverySubscriberGetsEveryAlert(t *testing.T) {
	r := New()
	tui, _ := r.Subscribe()
	api, _ := r.Subscribe()

	r.publish(Alert{SessionID: "s1"})

	if got := recvAlert(t, tui); got.SessionID != "s1" {
		t.Errorf("first subscriber got %+v", got)
	}
	// The bug this replaces: one consumer taking an alert off a shared channel
	// meant the other never saw it at all.
	if got := recvAlert(t, api); got.SessionID != "s1" {
		t.Errorf("second subscriber got %+v", got)
	}
}

func TestStalledSubscriberNeverBlocksPublish(t *testing.T) {
	r := New()
	stalled, _ := r.Subscribe()
	other, _ := r.Subscribe()

	done := make(chan struct{})
	go func() {
		for i := 0; i < eventBuffer*4; i++ {
			r.publish(Alert{SessionID: "s"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked on a subscriber that never read")
	}

	// Each subscriber overflowed into its own buffer. A shared channel would
	// have held eventBuffer alerts between the two of them, not each.
	if got := len(stalled); got != eventBuffer {
		t.Errorf("stalled subscriber buffered %d, want %d", got, eventBuffer)
	}
	if got := len(other); got != eventBuffer {
		t.Errorf("second subscriber buffered %d, want %d", got, eventBuffer)
	}
}

func TestUnsubscribeClosesAndStopsDelivery(t *testing.T) {
	r := New()
	ch, cancel := r.Subscribe()
	other, _ := r.Subscribe()

	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("cancelled subscription still open")
	}
	// Publishing after a cancel must not panic on the closed channel, and the
	// surviving subscriber must be unaffected.
	r.publish(Alert{SessionID: "s1"})
	if got := recvAlert(t, other); got.SessionID != "s1" {
		t.Errorf("survivor got %+v", got)
	}
	cancel()
}

func TestCloseEndsEverySubscription(t *testing.T) {
	r := New()
	ch, cancel := r.Subscribe()

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-ch; ok {
		t.Fatal("subscription outlived the receiver")
	}
	// Unsubscribing after Close, as a deferred cancel does, must be a no-op.
	cancel()
	r.publish(Alert{SessionID: "s1"})
}
