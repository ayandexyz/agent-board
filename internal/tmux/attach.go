package tmux

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"

	"github.com/creack/pty"
)

// Attachment is a viewer's own client on the board's tmux session: a
// grouped session sharing the windows but with its own current window and
// size, attached inside a PTY so bytes can be streamed to a remote screen.
type Attachment struct {
	Name string
	ptmx *os.File
	cmd  *exec.Cmd
	once sync.Once
}

// AttachCommands is the tmux invocation plan for a viewer named name on
// base: create the grouped session at the viewer's size, point it at the
// window when one is given, and the attach command itself, which the caller
// runs in a PTY.
func AttachCommands(base, name, windowID string, cols, rows int) (setup [][]string, attach []string) {
	setup = append(setup, []string{"tmux", "new-session", "-d", "-t", base, "-s", name,
		"-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows)})
	// Session option, so only the viewer is affected. tmux then asks xterm to
	// report wheel events, so a phone swipe scrolls whatever owns the pane:
	// the program if it tracks the mouse (full-screen TUIs), else tmux
	// copy-mode over the pane history. Without it a swipe on a shell pane
	// goes nowhere, since the client only holds the live screen.
	setup = append(setup, []string{"tmux", "set-option", "-t", name, "mouse", "on"})
	if windowID != "" {
		setup = append(setup, []string{"tmux", "select-window", "-t", name + ":" + windowID})
	}
	attach = []string{"tmux", "attach-session", "-t", name}
	return setup, attach
}

// ViewerName is base plus a random suffix, so two phones never share a
// current window.
func ViewerName(base string) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return base + "-view-" + hex.EncodeToString(b)
}

// Attach opens a grouped session on base, selects windowID when given, and
// attaches to it in a PTY of the given size.
func Attach(base, windowID string, cols, rows int) (*Attachment, error) {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	name := ViewerName(base)
	setup, attach := AttachCommands(base, name, windowID, cols, rows)
	for _, args := range setup {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			_ = exec.Command("tmux", "kill-session", "-t", name).Run()
			return nil, fmt.Errorf("tmux.attach %v: %w: %s", args[1:], err, out)
		}
	}
	cmd := exec.Command(attach[0], attach[1:]...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		_ = exec.Command("tmux", "kill-session", "-t", name).Run()
		return nil, fmt.Errorf("tmux.attach pty: %w", err)
	}
	return &Attachment{Name: name, ptmx: ptmx, cmd: cmd}, nil
}

func (a *Attachment) Read(p []byte) (int, error)  { return a.ptmx.Read(p) }
func (a *Attachment) Write(p []byte) (int, error) { return a.ptmx.Write(p) }

func (a *Attachment) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("tmux.resize: bad size %dx%d", cols, rows)
	}
	return pty.Setsize(a.ptmx, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

// Close detaches the viewer and kills its grouped session; the windows it
// looked at belong to base and are untouched.
func (a *Attachment) Close() error {
	a.once.Do(func() {
		_ = a.ptmx.Close()
		if a.cmd.Process != nil {
			_ = a.cmd.Process.Kill()
			_, _ = a.cmd.Process.Wait()
		}
		_ = exec.Command("tmux", "kill-session", "-t", a.Name).Run()
	})
	return nil
}
