# Mobile client: strategy

Date: 2026-09-12 (revised the same day: Android, so Expo, not a remux fork)

## Decision

The phone app is the user's existing Expo app, `~/Projects/AIChat` (Expo 57,
React Native 0.86, NativeWind, shadcn-style primitives). It talks HTTPS and
WebSocket straight to AgentBoard's loopback API, published to the tailnet
with `tailscale serve --bg 7878`. Tailscale supplies reachability from
cellular without opening ports and its ACL is the auth; the Go server never
binds anything but loopback and refuses browser origins.

A remux fork (Swift, SSH, tmux control mode) was the first plan and was
dropped because the user has no iPhone and the people around them are on
Android. Nothing from remux ports to React Native: its value is Citadel SSH
and GhosttyKit, neither of which exists there.

## What the Go side gives the phone

- `GET /api/v1/board` and `/sessions` for state, `/events` (SSE, `?after=`
  replay) to know when to refetch.
- `GET /api/v1/sessions/{id}/terminal` (WebSocket) for the live screen:
  the server attaches a grouped tmux viewer session in a PTY and pumps
  bytes; the phone renders them in xterm.js inside a WebView. Grouped so
  the phone's window switches and resizes never touch the desktop TUI.
- `POST /sessions/{id}/resolve` for permissions, `/input` for typing.

## Why the API lives in the TUI process

Active sessions, the busy map and held permission requests live in
`orchestrator.Service` and `activity.Receiver` memory. A separate daemon
would need all of that moved first.

## App architecture (AIChat)

Server state and client state are kept apart: TanStack Query owns
everything fetched from the board (cache, refetch, invalidation driven by
SSE events); Zustand owns what only the phone knows (saved hosts, the
active host, connection status). Screens are containers that compose
hooks; components under `features/*/ui` are pure and take props. Nothing
under `src/domain` imports React or React Native, so the rules there are
unit-tested in plain Jest.

```
src/
  core/         http client, sse reader, ws terminal transport, storage, config
  api/          typed client, one function per endpoint; types mirror internal/apitypes
  domain/       pure: column grouping, activity glyphs, waiting counts, connection reducer
  state/        zustand stores (hosts, connection) and react-query setup + keys
  features/
    hosts/      pick or add a board host          (screen + ui)
    board/      column cards over a ticket list   (screen + ui)
    ticket/     detail, start, status, merge      (screen + ui)
    terminal/   xterm.js WebView over the socket  (screen + ui + html bundle)
    waiting/    sessions needing a human, allow/deny
  navigation/   typed stack routes
```

The dependency rule points inward: features -> state/api -> core/domain.
`api` depends on `core` for transport; `domain` depends on nothing.

## Push is the gap

The app drops its connections in the background. Phase 3 adds an optional
ntfy topic next to the board's `notify-send`, not an APNs/FCM relay.

## Phases

1. Go: loopback API, SSE, WebSocket terminal, origin guard. Done.
2. AIChat: hosts, board, ticket, terminal, waiting screens on the API.
3. ntfy for background alerts; QR in the TUI carrying host and project.
