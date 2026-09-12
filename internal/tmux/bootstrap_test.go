package tmux

import (
	"reflect"
	"testing"
)

func TestBootstrapCommandsFreshSession(t *testing.T) {
	setup, attach := BootstrapCommands("board", "/bin/agentboard", false, false)
	if setup != nil {
		t.Errorf("fresh session should need no setup commands, got %v", setup)
	}
	want := []string{"tmux", "new-session", "-A", "-s", "board", "/bin/agentboard"}
	if !reflect.DeepEqual(attach, want) {
		t.Errorf("attach = %v, want %v", attach, want)
	}
}

func TestBootstrapCommandsStaleSessionWithoutTUIWindow(t *testing.T) {
	// new-session -A silently attaches without running the binary when the
	// session already exists, so a session left over from a crash (with
	// other windows still alive, the TUI window gone) needs a fresh window.
	setup, attach := BootstrapCommands("board", "/bin/agentboard", true, false)
	wantSetup := [][]string{
		{"tmux", "new-window", "-t", "board", "-n", "agentboard", "/bin/agentboard"},
	}
	if !reflect.DeepEqual(setup, wantSetup) {
		t.Errorf("setup = %v, want %v", setup, wantSetup)
	}
	want := []string{"tmux", "attach-session", "-t", "board"}
	if !reflect.DeepEqual(attach, want) {
		t.Errorf("attach = %v, want %v", attach, want)
	}
}

func TestBootstrapCommandsSessionWithLiveTUIWindow(t *testing.T) {
	setup, attach := BootstrapCommands("board", "/bin/agentboard", true, true)
	if setup != nil {
		t.Errorf("a live TUI window needs no setup commands, got %v", setup)
	}
	want := []string{"tmux", "attach-session", "-t", "board"}
	if !reflect.DeepEqual(attach, want) {
		t.Errorf("attach = %v, want %v", attach, want)
	}
}
