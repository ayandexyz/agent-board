package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ayandexyz/agent-board/internal/api"
	"github.com/ayandexyz/agent-board/internal/orchestrator"
)

// echoTerminal echoes every byte written to it back out, upper-cased, and
// records resizes, so a test can see both directions of the pump.
type echoTerminal struct {
	r, w    *io.PipeReader
	pw, ww  *io.PipeWriter
	resizes chan [2]int
	closed  chan struct{}
}

func newEchoTerminal() *echoTerminal {
	outR, outW := io.Pipe()
	return &echoTerminal{r: outR, pw: outW, resizes: make(chan [2]int, 4), closed: make(chan struct{})}
}
func (e *echoTerminal) Read(p []byte) (int, error) { return e.r.Read(p) }
func (e *echoTerminal) Write(p []byte) (int, error) {
	go e.pw.Write([]byte(strings.ToUpper(string(p))))
	return len(p), nil
}
func (e *echoTerminal) Resize(c, r int) error { e.resizes <- [2]int{c, r}; return nil }
func (e *echoTerminal) Close() error {
	select {
	case <-e.closed:
	default:
		close(e.closed)
		e.pw.Close()
	}
	return nil
}

type fakeOpener struct {
	term       *echoTerminal
	lastWindow string
	lastSize   [2]int
}

func (f *fakeOpener) Open(windowID string, cols, rows int) (api.Terminal, error) {
	f.lastWindow, f.lastSize = windowID, [2]int{cols, rows}
	return f.term, nil
}

func TestTerminalWithoutBackendIs501(t *testing.T) {
	ts, _ := newTestServer(t, newFakeBoard(), nil)
	resp, _ := do(t, ts, "GET", "/api/v1/terminal", nil)
	if resp.StatusCode != 501 {
		t.Errorf("status %d", resp.StatusCode)
	}
}

func TestSessionTerminalStreamsBothWaysAndResizes(t *testing.T) {
	b := newFakeBoard()
	b.sessions = []*orchestrator.AgentSession{{SessionID: "SES-01", TicketID: "AGT-01", WindowID: "@7"}}
	opener := &fakeOpener{term: newEchoTerminal()}
	s := &api.Server{Board: b, Terminals: opener}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/sessions/SES-01/terminal?cols=120&rows=40"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	if opener.lastWindow != "agent-SES-01" || opener.lastSize != [2]int{120, 40} {
		t.Errorf("opened window %q size %v", opener.lastWindow, opener.lastSize)
	}

	if err := conn.Write(ctx, websocket.MessageBinary, []byte("ls\r")); err != nil {
		t.Fatal(err)
	}
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if typ != websocket.MessageBinary || string(data) != "LS\r" {
		t.Errorf("got %v %q", typ, data)
	}

	msg, _ := json.Marshal(map[string]interface{}{"type": "resize", "cols": 90, "rows": 30})
	if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-opener.term.resizes:
		if got != [2]int{90, 30} {
			t.Errorf("resize = %v", got)
		}
	case <-ctx.Done():
		t.Fatal("resize never reached the terminal")
	}

	conn.Close(websocket.StatusNormalClosure, "")
	select {
	case <-opener.term.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal not closed after the socket closed")
	}
}

func TestSessionTerminalUnknownSessionIs404(t *testing.T) {
	s := &api.Server{Board: newFakeBoard(), Terminals: &fakeOpener{term: newEchoTerminal()}}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	resp, _ := do(t, ts, "GET", "/api/v1/sessions/NOPE/terminal", nil)
	if resp.StatusCode != 404 {
		t.Errorf("status %d", resp.StatusCode)
	}
}

func TestRequestsWithAnOriginAreRefused(t *testing.T) {
	ts, _ := newTestServer(t, newFakeBoard(), nil)
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/tickets", strings.NewReader(`{"title":"x"}`))
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("status %d, want 403", resp.StatusCode)
	}
	req, _ = http.NewRequest("GET", ts.URL+"/api/v1/board", nil)
	req.Header.Set("Origin", "http://127.0.0.1:7878")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("same-origin GET status %d, want 403 (no browser front end exists yet)", resp.StatusCode)
	}
}
