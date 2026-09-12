package tmux

import (
	"reflect"
	"strings"
	"testing"
)

func TestAttachCommandsGroupsAndSelectsWindow(t *testing.T) {
	setup, attach := AttachCommands("demo", "demo-view-ab12", "@3", 100, 40)
	want := [][]string{
		{"tmux", "new-session", "-d", "-t", "demo", "-s", "demo-view-ab12", "-x", "100", "-y", "40"},
		{"tmux", "select-window", "-t", "demo-view-ab12:@3"},
	}
	if !reflect.DeepEqual(setup, want) {
		t.Errorf("setup = %v, want %v", setup, want)
	}
	if !reflect.DeepEqual(attach, []string{"tmux", "attach-session", "-t", "demo-view-ab12"}) {
		t.Errorf("attach = %v", attach)
	}
}

func TestAttachCommandsWithoutWindow(t *testing.T) {
	setup, _ := AttachCommands("demo", "demo-view-1", "", 80, 24)
	if len(setup) != 1 {
		t.Errorf("no window should mean no select-window, got %v", setup)
	}
}

func TestViewerNameIsUniquePerCall(t *testing.T) {
	a, b := ViewerName("demo"), ViewerName("demo")
	if a == b || !strings.HasPrefix(a, "demo-view-") {
		t.Errorf("names %q %q", a, b)
	}
}
