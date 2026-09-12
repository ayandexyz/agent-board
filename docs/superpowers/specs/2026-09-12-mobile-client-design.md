# Mobile client: strategy

Date: 2026-09-12

## Decision

The phone app is a fork of remux (Swift, iOS, tmux control mode over SSH via
Citadel, GhosttyKit rendering), kept in its own repository. AgentBoard exposes
a loopback HTTP API (`internal/api`) that the phone reaches over the same SSH
connection it already uses for tmux. Tailscale is the network layer, not an
alternative to SSH: it gives reachability from cellular without opening ports
and, with Tailscale SSH, identity. Plain SSH keys are the fallback.

We deliberately do not copy agent-orchestrator's LAN listener, bearer token,
QR pairing and Cloudflare tunnel. That machinery exists because their client
is xterm.js in a WebView with no native terminal path. Ours has tmux control
mode, so the whole auth surface is "can you SSH in".

## Why the API lives in the TUI process

Active sessions, the busy map and the held permission requests live in
`orchestrator.Service` and `activity.Receiver` memory. SQLite holds the rest.
A separate daemon would need all of that moved out first; a loopback server
in the same process needs nothing moved.

## How the phone attaches

- One SSH connection, two channels: a `direct-tcpip` forward to
  `127.0.0.1:7878` (remux's `RemuxSessionLiveForward` pattern) and an exec
  channel running `tmux -C`.
- Attach as a **grouped session**:
  `tmux -C new-session -t <project> -s <project>-phone`. Attaching to the
  same session would make the phone's `select-window` change the desktop's
  current window and, under tmux's default `window-size latest`, shrink the
  desktop TUI to phone width.
- `GET /api/v1/sessions` returns the tmux window per session; the phone runs
  `select-window -t <window_id>` in its own grouped session.
- Permission requests: `GET /board` and `GET /sessions` carry `activity`
  and `decidable`; the phone posts `allow`/`deny` to
  `/sessions/{id}/resolve`, the same path as the TUI's `y`/`n`.

## Push is the gap SSH cannot fill

iOS drops the SSH connection in the background, so "agent is waiting" needs
a push path. The board already fires `notify-send`; phase 3 adds an optional
ntfy topic next to it rather than an APNs relay.

## Phases

1. Go: `internal/api` on loopback with SSE. Done. Verified with curl and
   `ssh -L`.
2. iOS: fork remux, add a Board tab. Ticket tap opens the agent's window in
   the grouped session.
3. Permission sheet, ntfy for background alerts, optional QR in the TUI
   carrying only host and project name.

## iOS folder structure (on top of remux's App/Domain/SSH/Tmux/Ghostty/Persistence)

```
RemuxApp/Sources/Board/
  BoardConnectionCoordinator.swift   SSH lease -> direct-tcpip forward -> API client
  BoardAPIClient.swift               REST over the forward
  BoardEventStream.swift             SSE reader with cursor and reconnect
  BoardModels.swift                  Ticket, Session, PendingRequest (from internal/apitypes)
  BoardStore.swift                   @Observable state reduced from events
  BoardView.swift                    kanban columns
  TicketDetailView.swift             prompt, status, activity glyph, "Open pane"
  PermissionSheet.swift              allow / deny -> POST resolve
```

Reuse from remux: `RemuxSessionLiveForward`, `RemuxSSHExecSession.run`,
`SSHTmuxControlCommandBuilder`, `TmuxSessionController.selectWindow`,
`ShortcutExecutor` for "type a prompt then Enter".
