package keybinding

import (
	"strings"
	"testing"
)

func TestDefaultKeyMapMergeTicket(t *testing.T) {
	km := DefaultKeyMap()

	b, ok := km.Lookup("m")
	if !ok {
		t.Fatal("m should be bound by default")
	}
	if b.Action != ActionMergeTicket {
		t.Errorf("m bound to %v, want ActionMergeTicket", b.Action)
	}
	if b.Description != "merge ticket" {
		t.Errorf("description = %q, want %q", b.Description, "merge ticket")
	}
}

func TestMergeTicketActionString(t *testing.T) {
	if got := ActionMergeTicket.String(); got != "merge_ticket" {
		t.Errorf("ActionMergeTicket.String() = %q, want merge_ticket", got)
	}
}

func TestTicketViewHelpIncludesMerge(t *testing.T) {
	km := DefaultKeyMap()
	help := km.TicketViewHelp()
	if !strings.Contains(help, "m: merge ticket") {
		t.Errorf("ticket view help should mention merge, got %q", help)
	}
}
