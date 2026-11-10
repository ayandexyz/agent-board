package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

func fakeListing(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FAKE_TMUX_DIR", dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
}

func TestPaneListingRelistsBeforeCallingAPaneGone(t *testing.T) {
	fakeListing(t, `#!/bin/sh
n=$(( $(cat "$FAKE_TMUX_DIR/n" 2>/dev/null || echo 0) + 1 ))
echo "$n" > "$FAKE_TMUX_DIR/n"
printf '%%1:10:0:claude\n'
[ "$n" -ge 2 ] && printf '%%2:20:0:opencode\n'
exit 0
`)
	var l paneListing

	if st, ok, err := l.status("%1"); err != nil || !ok || st.command != "claude" {
		t.Fatalf("status(%%1) = %+v, %v, %v", st, ok, err)
	}
	st, ok, err := l.status("%2")
	if err != nil || !ok || st.command != "opencode" {
		t.Fatalf("status(%%2) = %+v, %v, %v; want the pane created after the cached listing", st, ok, err)
	}
}

func TestPaneListingReportsDeadAndMissingPanes(t *testing.T) {
	fakeListing(t, "#!/bin/sh\nprintf '%%1:10:1:bash\\n'\n")
	var l paneListing

	if st, ok, _ := l.status("%1"); !ok || !st.dead {
		t.Fatalf("status(%%1) = %+v, %v; want a dead pane", st, ok)
	}
	if _, ok, err := l.status("%9"); ok || err != nil {
		t.Fatalf("status(%%9) found = %v, err = %v; want a missing pane without error", ok, err)
	}
}

func TestPaneListingSurfacesTmuxFailure(t *testing.T) {
	fakeListing(t, "#!/bin/sh\nexit 1\n")
	var l paneListing

	if _, _, err := l.status("%1"); err == nil {
		t.Fatal("status() err = nil, want the failed listing")
	}
}
