# TODO

## Mobile companion app (chat-style agent control, T3/freecode-style protocol capture)

Goal: mobile app with chat-like interface to view the kanban board, see running
agents, chat/approve/answer questions per agent, get notifications.

UI/logic separation is already fine (`internal/orchestrator` has no dependency
on `internal/tui`; `tui` only touches orchestrator via its interfaces). What's
missing is an API layer and a data contract expressive enough for a chat UI.

### Gaps found (2026-08-19)

1. **`internal/api` doesn't exist yet.** AGENT.md describes it as if built
   (chi router, handlers, WS hub) — it isn't. Needs to be stood up wrapping
   the same `orchestrator.Service` the TUI uses, no new logic.

2. **Logs are terminal lines, not chat messages.** `Service.AppendLog` /
   `GetLogs` (internal/orchestrator/service.go) is `map[string][]string`,
   capped at 1000 lines, fed by a raw `Reporter func(string)` callback from
   PTY/tmux output. No role (agent/user/system), no message boundaries.
   → Replace with a typed `AgentEvent{Kind, Role, Content, Timestamp}` and
   `AppendEvent(sessionID string, ev AgentEvent)`. `Kind` should distinguish
   plain output vs. a question the agent is waiting on vs. an approval
   request, so mobile can render chat bubbles / trigger notifications /
   show an Approve button appropriately.

3. **`SendInput` only works for `TmuxRunner`** (service.go, type-asserts on
   `s.runner`). PtyRunner is the primary spawn path per AGENT.md, so
   "reply to agent from mobile" is broken for it today. Needs `SendInput`
   added to `AgentRunner`/`PtyRunner`.

4. **No "agent is waiting for input" signal.** `ApproveProposal`
   (approval.go) only covers pre-run plan approval. Mid-run "agent asked a
   question, notify the phone" doesn't exist. Needs idle/waiting-state
   detection (PtyRunner already has some idle-pattern detection per
   AGENT.md) turned into an event, not just a log line.

5. **No persistence for logs/events.** Current log buffer is in-memory only,
   lost on process restart — bad for a "check the app later" mobile
   pattern. There's already an `Events` table (`store.CreateEvent`, used for
   proposal approvals) — extend that pattern to persist agent messages and
   completions instead of introducing a new mechanism.

### Suggested order

1. Define `AgentEvent` type + persist via `Events` table (fixes #2 + #5).
2. Add `SendInput` to `PtyRunner` (fixes #3).
3. Add waiting-for-input detection → event (fixes #4).
4. Stand up minimal `internal/api` (chi + WS) wrapping `orchestrator.Service`
   as the second consumer, proving the interfaces are UI-agnostic (fixes #1).
5. Then build the mobile chat UI against the API, capturing per-agent
   spawn/session protocol the way t3code/freecode already do.

## Worktree/merge follow-ups (worktree isolation shipped 2026-09-07)

Shipped: per-ticket git worktrees, review→done merge gate (local rebase+ff or
PR via gh, per-merge override with `s` in the modal), status/merge/delete
routed through `orchestrator.Service` (TUI is presentation-only for these).

Follow-ups:

1. ~~**`ActionMergeTicket` keybinding**~~ — done (2026-09-08): `m` in the
   ticket detail view merges a Review ticket (or moves it to done when there is
   nothing to merge). Rebindable via `[tui.keybindings] merge_ticket`.
2. **Feed merge conflicts back to the agent** — on `merge.conflict`, offer
   "send to agent": spawn/reuse the ticket's agent in its worktree with a
   "rebase onto <base> and resolve conflicts" prompt (mirrors AO's feedback
   routing).
3. **PR polling** — background poll of `gh pr view` for tickets with `pr_url`
   so merged PRs auto-advance to done (needs the API/event layer anyway).
4. **Route ticket create/edit through the service** — delete/status/merge are
   done; create/edit still write to the store directly from the TUI.
