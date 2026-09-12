# Orchestrator Bug Report

Date: 2026-09-08
Scope: spawn, monitor, stop, completion, merge, store. Found during the activity-tracking research pass; each item was verified by reading the code. Line numbers refer to commit `290b60c`.

`go vet ./...` is clean. `go test ./...` fails in `internal/orchestrator` with three order-dependent tests that pass in isolation. Bug 2 is the cause.

## Blocking (fix before activity tracking)

### 1. Nil pointer panic on unknown agent
**Fixed** on `fix/orchestrator-blocking-bugs`: `Start` returns `ErrUnknownAgent` before any window is created.

`internal/orchestrator/pty_runner.go:64`. `cfg := pty.GetConfig(req.Agent)` is nil for any agent outside the six registry keys. Line 52 guards `cfg != nil`, line 64 dereferences `cfg.Bin` unconditionally. Assigning a ticket to `cursor`, which `config.DetectAgents` advertises, crashes the whole TUI.

Fix: return an error when `cfg == nil`. Add `cursor` to the registry or drop it from detection.

### 2. Monitor goroutine can never be cancelled
**Fixed**: `RunHandle.Stop` cancels the monitor and prompt injection; `StopSession` calls it.

`internal/orchestrator/pty_runner.go:138`. `case <-context.Background().Done():` is a channel that never fires. Each session leaks a goroutine executing three tmux commands every 500 ms forever. In tests the leaked goroutines from earlier cases consume the next case's fake tmux counters, which is the flaky failure.

Fix: plumb the request context and a per-session cancel func through `RunHandle`; cancel in `StopSession`.

### 3. Stopping a session does not stop the agent
**Fixed**: the runner keeps `sessionID -> paneID`, `Kill` runs `tmux kill-pane`, `GetPaneID` is real.

`internal/orchestrator/service.go:377-396`. `StopSession` calls `StopPane` on `TmuxRunner`, but the live path never registers panes there, so `RemovePane` returns nil at `pane_manager.go:340`. The pane keeps running. When the agent later exits, the leaked monitor from bug 2 calls `onComplete`, which overwrites the `cancelled` session as `completed` and moves the ticket to review.

Fix: `tmuxAgentRunner` keeps `sessionID -> paneID`; `StopSession` cancels the monitor and runs `tmux kill-pane`.

### 4. Ticket stuck active forever
**Fixed**: the no-runner branch falls through to the error path that ends the session and clears the flag.

`internal/orchestrator/service.go:148-150` and `303-305`. `SetAgentActive(true)` runs at line 107 / 264. The `no runner configured` branch returns without `EndSession` or `SetAgentActive(false)`. The ticket is permanently flagged, which blocks `MergeTicket` at `merge.go:74` and arms the animation ticker forever.

Fix: undo both on that path, same as the other error branches.

## High

### 5. Completion attributed to the wrong ticket
**Fixed** on `fix/completion-delivery`: one board-wide listener reports the ticket and session carried by the completion; the start commands no longer listen.

`internal/tui/app.go:537`. `startDirectRunAndListenCmd` discards the received `RunCompletion` and reports its own closure's ticket id. With two concurrent runs whichever listener wins the channel claims the other's completion. The sibling functions at 495 and 516 use `completion.TicketID` correctly.

### 6. Completion channel deadlock
**Fixed**: `SubscribeCompletions` gives each consumer its own 64-deep buffer with non-blocking sends; `onComplete` can never block.

`internal/orchestrator/service.go:49` and `128-135`. `completionCh` holds 16. The TUI reads one value per started run and abandons the read after 60 s. After 16 unread completions every later `onComplete` blocks forever on the send, holding the pane state.

Fix: replace the shared channel with the event hub from the tracking design, or make the send non-blocking and drive the TUI from a subscription.

### 7. Prompt injection forks one tmux process per character
**Fixed** on `fix/guarded-send-input`: `sendKeysChunked` sends `send-keys -l` in 16 KiB chunks, pauses 300 ms, then `Enter`; cancellation stops it before Enter.

`internal/orchestrator/pty_runner.go:93-104`. One `exec.Command` per rune with a 10 ms sleep. A 2 KB prompt is about 2000 forks and 20 s. Any single `send-keys` error returns mid-prompt with no error surfaced and no `onComplete`, while the monitor keeps running.

Fix: `send-keys -l` in 16 KB chunks, 300 ms pause, then `Enter`. Report failure through `onComplete`.

### 8. Merge races an active agent
**Fixed** on `fix/merge-safety`: `Service.reserve` claims the ticket under `s.mu` together with the active-session check; runs refuse to start while a merge or delete holds it (`ErrTicketBusy`), and merges, PR-finalize and delete refuse while a run is starting or active.

`internal/orchestrator/merge.go:71-76`. Active-session check, then rebase and merge with no lock held. A run started in between rebases a worktree the agent is writing to.

Fix: hold `s.mu` across the check and mark the ticket merging; `StartAdHocRun` refuses while merging.

### 9. Unsynchronised PTY state machine
`internal/pty/pty.go:106,126,131-136,159-164,176`. `canCheckCompletion` and `current` are plain values written from `time.AfterFunc` goroutines and read from the read loop. A never-observed update leaves the machine in `working` forever. Not caught because nothing runs `runStateMachine` under `-race`.

## Medium

### 10. Pane switching never works
**Fixed** with bug 3; `SwitchToPane` now uses `select-window` on the pane target.

`internal/orchestrator/pty_runner.go:210-212`. `GetPaneID` is a stub returning empty though the pane id is known at line 46. Fixing bug 3 fixes this.

### 11. Done ticket keeps a stale worktree
**Fixed**: `finalizeMerge` cleans up first and only then moves the ticket to done; a failed cleanup keeps the ticket in review with its path intact and the next merge retries it.

`internal/orchestrator/merge.go:145-163`. `MoveStatus(done)` commits before `Cleanup`; on cleanup failure only an event is logged and `SetWorktreePath("")` is skipped. Nothing ever reclaims it.

### 12. SQLite without busy_timeout or WAL
**Fixed** on `fix/sqlite-pragmas`: the DSN carries `busy_timeout(5000)`, `journal_mode(WAL)` and `foreign_keys(1)`; a second process waits for the write lock instead of failing.

`internal/store/sqlite.go:30-40`. A second agentboard process on the same database hits `SQLITE_BUSY` immediately. The completion path discards those errors (`summarizer.go:402-414`).

Fix: `_pragma=busy_timeout(5000)` and `journal_mode(WAL)` in the DSN.

### 13. Events written with the request context
`internal/orchestrator/service.go:85,171-176`. `worktree.created` and `run.started` use the caller's ctx with the error discarded. A cancelled caller silently loses the event while the session and monitor live on.

### 14. Data race on AgentPane
`internal/orchestrator/pane_manager.go:251-259` vs `296-305`. `monitorPane` writes fields under `pm.mu`; `ListPanes` and `GetPane` hand out raw pointers read without the lock. Latent only because `CreatePane` is unreachable.

### 15. CreatePane holds the write lock across six execs
`internal/orchestrator/pane_manager.go:106-108`. Render-loop reads block for the duration of tmux startup. Same latent status as 14.

## Notes

- `PaneManager.CreatePane` and `PaneManager.monitorPane` are dead in the live wiring; the board always uses `tmuxAgentRunner`. Consider deleting them after bug 3 lands.
- The effective ready timeout is always 3 s because `waitForReady` takes `max(cfg.ReadyWait, 3s)` and every `ReadyWait` is under 3 s.

---

## Addendum: FreeCode repo (found 2026-09-08 during Phase 5 implementation)

### F1. `registerRtkHook()` is silently unregistered at bootstrap

`apps/core/src/hooks/bootstrap.ts` calls `registerRtkHook()`, which registers with source `"settings"`. `initHooks` then constructs `HookSettingsManager` and calls `load()`, and `apps/core/src/hooks/settings.ts:227` opens with `unregisterAllHooks("settings")`. That wipes the hook that was just registered.

Verified: after `initHooks(root)`, `getHooksForEvent("PreToolUse")` returns `[]`. The rtk bash-rewrite feature has never run through this path, so no bash command is being rewritten to its compact `rtk` equivalent and the token saving it exists for is not being realised. The same trap caught the board webhook, which is why that one registers with source `"session"` instead.

Fix is one word, `"settings"` to `"session"` at the `registerHook` call in `builtin/rtk-rewrite.ts`. Deliberately not applied: it would switch on bash-command rewriting for the first time, which changes what every shell tool call actually executes. That is a behaviour change for the repo owner to make, not a drive-by.

### F2. `npm run check-types` exits 1 without printing an error

In `apps/core`, both `npm run check-types` and `npx tsc --noEmit -p tsconfig.json` exit 1 and print only two `npm notice run` lines. Running the compiler directly (`node node_modules/.pnpm/typescript@5.9.2/node_modules/typescript/bin/tsc --noEmit -p tsconfig.json`) exits 0 with no diagnostics. Present at baseline with no local changes, so some wrapper on `npx`/`npm run` is swallowing the invocation. CI gating on that script would be gating on nothing.

### F3. `unregisterAllHooks()` is called with no argument in tests

`apps/core/src/hooks/hook-payloads.test.ts` calls `unregisterAllHooks()`, but the signature at `registry.ts:156` requires `source`. It typechecks only because the direct-compiler run above is the only thing that would catch it and tests may fall outside the project's `tsconfig` include. The call clears nothing, so those tests rely on registration order rather than a clean registry.
