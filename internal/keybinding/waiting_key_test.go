package keybinding

import "testing"

func TestWaitingKeyBound(t *testing.T) {
	km := DefaultKeyMap()
	b, ok := km.Lookup("W")
	if !ok || b.Action != ActionShowWaiting {
		t.Fatalf("W bound to %v, want ActionShowWaiting", b.Action)
	}
	if b.Description != "waiting agents" {
		t.Errorf("description = %q", b.Description)
	}
	if got := ActionShowWaiting.String(); got != "show_waiting" {
		t.Fatalf("String() = %q", got)
	}
}

func TestWaitingKeyRebindable(t *testing.T) {
	km := DefaultKeyMap()
	ApplyConfig(&km, map[string]string{"show_waiting": "!"})
	b, ok := km.Lookup("!")
	if !ok || b.Action != ActionShowWaiting {
		t.Fatalf("! bound to %v after rebinding, want ActionShowWaiting", b.Action)
	}
	if _, still := km.Lookup("W"); still {
		t.Fatal("old key still bound after rebinding")
	}
}
