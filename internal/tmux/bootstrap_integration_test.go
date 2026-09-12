//go:build integration

package tmux

import (
	"os/exec"
	"testing"
)

func TestSessionExistsAndWindowExists(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	name := "bstest-" + ViewerName("x")[8:]
	if SessionExists(name) {
		t.Fatalf("session %q should not exist yet", name)
	}
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-n", "agentboard", "sh").CombinedOutput(); err != nil {
		t.Fatalf("new-session: %v: %s", err, out)
	}
	defer exec.Command("tmux", "kill-session", "-t", name).Run()

	if !SessionExists(name) {
		t.Errorf("session %q should exist", name)
	}
	if !WindowExists(name, "agentboard") {
		t.Errorf("window %q should exist in %q", "agentboard", name)
	}
	if WindowExists(name, "does-not-exist") {
		t.Errorf("window %q should not exist in %q", "does-not-exist", name)
	}
}
