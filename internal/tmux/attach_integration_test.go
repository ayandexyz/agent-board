//go:build integration

package tmux

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAttachStreamsAGroupedSessionAndCleansUp(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	base := "abtest-" + ViewerName("x")[8:]
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", base, "-x", "80", "-y", "24", "sh").CombinedOutput(); err != nil {
		t.Fatalf("base session: %v: %s", err, out)
	}
	defer exec.Command("tmux", "kill-session", "-t", base).Run()

	a, err := Attach(base, "", 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Write([]byte("echo hello-from-phone\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var seen strings.Builder
	buf := make([]byte, 4096)
	for time.Now().Before(deadline) && !strings.Contains(seen.String(), "hello-from-phone") {
		n, err := a.Read(buf)
		if err != nil {
			break
		}
		seen.Write(buf[:n])
	}
	if !strings.Contains(seen.String(), "hello-from-phone") {
		t.Fatalf("never saw the echo; got %q", seen.String())
	}
	if err := a.Resize(90, 20); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if !strings.Contains(string(out), a.Name) {
		t.Fatalf("viewer session %q not listed in %q", a.Name, out)
	}
	_ = a.Close()
	out, _ = exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if strings.Contains(string(out), a.Name) {
		t.Errorf("viewer session %q still alive after Close", a.Name)
	}
	if !strings.Contains(string(out), base) {
		t.Errorf("base session %q was killed with the viewer", base)
	}
}
