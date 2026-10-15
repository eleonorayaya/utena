# Browser Parking (PR 2) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** The attached session's browser windows live on the browser workspace (`2`). Every other session-owned window is parked on `P`. The swap is quiet and never changes focus. Unowned windows are never moved.

**Stacks on:** PR 1 (`eqt/utena-window-management`, utena#124). This branch is `eqt/utena-window-parking`.

**Architecture:**
- A new `internal/wm` package holds the OS window-manager seam: the `Driver` interface, `Window`, a `FakeDriver`, and a `None` driver.
- `internal/aerospace` implements `wm.Driver` with the `aerospace` CLI.
- `browser.Service` gains placement:
  - It links each owned Chrome window to its `wm` window ID, in memory.
  - It works out where each window belongs: the browser workspace for the attached session, the park workspace otherwise.
  - It moves only windows that are in the wrong place.
- The trigger for a swap is a new `session.attached` eventbus event, published by `session.SessionService` every time it changes which session is attached. Swaps run on a single worker goroutine and coalesce, keeping only the latest target.
- The daemon flags `--wm`, `--browser-workspace` and `--park-workspace` configure it.

**Spec:** `docs/superpowers/specs/2026-10-15-browser-window-management-design.md`. See §Event flow, §Linking, the "PR 1 amendments" and the "PR 1 smoke findings". Decisions in this plan that refine the spec are recorded in Task 8:
- `wm.Driver` originally had no focus method, on the assumption that AeroSpace follows Chrome's own focus. Live testing disproved that (Chrome cannot reliably activate itself while another app is frontmost), so `wm.Driver` gained `FocusWindow` and `Open` focuses explicitly through it; see the spec's PR 2 amendments.
- `browser.Driver` gains `AppBundleID()` and `WindowTitle(Window) string`. The second returns the title the OS shows for that window, for Chrome `"<active tab title> - Google Chrome"`. This keeps the title format inside the driver.
- Linking order: (1) a cached link that is still live; (2) for windows utena creates, the AeroSpace window that appeared since a snapshot taken just before `CreateWindow`, polled for up to 1.5 s, ignoring wm IDs that are already linked; (3) an exact title match among unlinked wm windows, only when no other unlinked Chrome window shares that title. There is **no elimination step**. AeroSpace lists every Chrome window (popups, PiP, undocked DevTools, other profiles), while the extension reports only normal windows, and AeroSpace can lag behind the extension. So "the last unlinked window" can be a foreign window, and no counting heuristic is safe. If the match is still ambiguous, **don't move anything**: log it at debug level and retry on the next `TabsChanged` or swap. The marker-tab fallback in the spec is deferred until real ambiguity shows up.
- An activating `Open` that reuses an existing window runs placement synchronously, under its lock, before it touches tabs or focuses the window. Focusing a window still parked on `P` (through `wm.FocusWindow`) would switch the user's view to `P`. Placing before the tab change also keeps the window's title matching its wm title.
- The created-window diff is checked on the Chrome side as well. After `linkCreated` succeeds, `Open` lists Chrome windows once. If any Chrome window other than the one it created appeared since its earlier list, it drops the link, because the new wm window may belong to that other window. Title matching may link it later.
- Detach never clears the attached session. The browser keeps the last attached session's windows on the browser workspace (there is no `SessionAttached{0}`).
- Creating windows minimized to avoid the flash is not attempted. A background-session window may briefly appear where AeroSpace's `on-window-detected` rule puts it (`2`), then it is moved to `P`.

**Rules:**
- Run everything through `task` from the repo root: `task test --force`, `task fmt`. Never run `go` directly, and never `cd` into subdirectories.
- `task lint` is unavailable locally because `golangci-lint` is not installed. Keep the code lint-clean by inspection.
- No code comments.
- Work TDD: write a failing test, watch it fail, implement, watch it pass.
- Commit once per task. Every commit message ends with a blank line, then `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
- Never run `task install`, `tui:install` or `daemon:install`.
- Known pre-existing failure: `internal/session` `TestSessionService_AddWorkspace_FullIntegration` also fails on `main`. Ignore it.

---

### Task 1: The `wm` package

**Files:** create `internal/wm/driver.go` and `internal/wm/testing.go`, plus a small test.

```go
package wm

type Driver interface {
	ListWindows(ctx context.Context, bundleID string) ([]Window, error)
	MoveToWorkspace(ctx context.Context, windowID, workspace string) error
}

type Window struct {
	ID        string
	Title     string
	Workspace string
}

type None struct{}
```

- `None.ListWindows` returns `nil, nil`, and `None.MoveToWorkspace` returns `nil`. Add `var _ Driver = None{}`.
- `FakeDriver` is a mutex-guarded in-memory set of windows with these methods:
  - `Add(id, title, workspace string)`
  - `SetTitle(id, title string)`
  - `Remove(id string)`
  - `Workspace(id string) string`
  - `Moves() []Move`, where `Move` is `{WindowID, Workspace}`
  - `MoveErr error`
  - `ListWindows` ignores `bundleID`. The fake holds only the windows the test adds.
  - `MoveToWorkspace` records the move, updates the window's workspace, and returns an error if the window is unknown.
- Test: `None` is inert, and the fake records moves and updates workspaces.

Commit: `feat(wm): window manager driver contract, fake and no-op driver`

### Task 2: The AeroSpace driver (`internal/aerospace`)

**Files:** `internal/aerospace/driver.go` and `driver_test.go`.

- `type Runner interface { Run(ctx context.Context, name string, args ...string) ([]byte, error) }`, plus an `execRunner` that uses `exec.CommandContext(...).Output()`. For a non-zero exit, include stderr in the error, using `exec.ExitError.Stderr`.
- `NewDriver() *Driver` uses `execRunner`. `newDriver(r Runner)` is for tests.
- `ListWindows` runs:

  ```bash
  aerospace list-windows --monitor all --app-bundle-id <id> --json --format '%{window-id} %{window-title} %{workspace}'
  ```

  It then decodes `[{"window-id":int,"window-title":string,"workspace":string}]` into `wm.Window`, converting the ID with `strconv.Itoa`. An empty bundle ID is an error.
- `MoveToWorkspace` runs:

  ```bash
  aerospace move-node-to-workspace --window-id <id> <workspace>
  ```

  It never uses `--focus-follows-window`.
- Tests use a fake runner that records argv and returns canned output, including the real JSON sample below:
  - the exact argv for both commands;
  - JSON decoding;
  - a runner error propagates;
  - an empty bundle ID is rejected.

```json
[{"window-id":70770,"window-title":"Extensions - utena - Google Chrome","workspace":"2"},{"window-id":70977,"window-title":"fix(chrome): polka dots … - Google Chrome","workspace":"3"}]
```

Commit: `feat(aerospace): wm.Driver over the aerospace CLI`

### Task 3: How the browser driver describes its windows

**Files:** `internal/browser/driver.go`, `internal/browser/testing.go`, `internal/chrome/driver.go` and tests.

- Add two methods to `browser.Driver`:
  - `AppBundleID() string`
  - `WindowTitle(w Window) string`
- Chrome returns `"com.google.Chrome"`. `WindowTitle` returns the active tab's title + `" - Google Chrome"`, or `""` when there is no active tab or its title is empty.
- `FakeDriver`:
  - `AppBundleID()` returns `"fake.browser"`.
  - `WindowTitle` returns the active tab's `Title` + `" - Fake"`.
  - Add `SetTitle(windowID, title string)`, which sets the active tab's title.
- Tests: Chrome's `WindowTitle` with an active tab, without one, and with an empty title.

Commit: `feat(browser): drivers describe their app bundle id and OS window title`

### Task 4: The `session.attached` event

**Files:** `internal/eventbus/events.go`, `internal/session/sessionservice.go` and tests.

- Add `SessionAttached = "session.attached"` and `type SessionAttachedEvent struct { SessionID uint }`.
- `SessionService` publishes it **after** the store update in each place that sets `IsAttached = true`:
  - `handleMuxClientSessionChanged`
  - `handleMuxClientAttached`
  - `ActivateSession`: after `s.store.Update(session)` succeeds. Keep the existing early return when the session is already attached; nothing changes there.
- If publishing fails, log it with `slog.Warn` and don't return the error. That matches how `SessionActivated` is handled.
- Tests: use the existing session-service test helpers (look at how the tests for `handleMuxClientSessionChanged` and `ActivateSession` are built). Subscribe a recorder to the bus and assert that each path publishes exactly one `SessionAttachedEvent` with the right ID.

Commit: `feat(session): publish session.attached when the attached session changes`

### Task 5: Linking Chrome windows to `wm` windows

**Files:** `internal/browser/link.go` and `link_test.go`.

This is a pure function plus a small cache, kept separate from `Service` so it can be tested in isolation.

```go
type linker struct {
	links map[string]string
}

func (l *linker) resolve(chrome []Window, title func(Window) string, wmWindows []wm.Window, want []string) map[string]string
```

- `want` lists the Chrome window IDs to link. The result maps each linked Chrome ID to its wm ID. IDs that can't be linked unambiguously are left out.
- Steps:
  1. Drop any cached link whose wm ID is no longer in `wmWindows`, or whose Chrome ID is no longer in `chrome`.
  2. For each wanted ID that has a valid cached link, use it.
  3. Set aside the "claimed" wm IDs, meaning every cached link, including links for Chrome windows that aren't wanted. The unclaimed wm windows are the candidates.
  4. **No elimination.** Never link a window because it is the last one left. AeroSpace lists Chrome windows the extension never reports, and it can lag behind the extension.
  5. **Title:** for each remaining wanted ID with a non-empty `title(w)`, link it if exactly one candidate has `Title == title(w)` and no other Chrome window in `chrome` without a cached link (wanted or not) has the same title.
  6. Store new links in the cache.
- Add `func (l *linker) linkCreated(chromeID string, before, after []wm.Window) bool`. When exactly one wm ID is in `after` but not in `before` and not already linked in the cache, it caches that link and returns true.
- Table tests:
  - a cache hit;
  - a stale cache entry is dropped;
  - no elimination: the last unlinked window is not guessed;
  - a unique title;
  - a duplicate title gives no link;
  - a title already claimed by another link;
  - a title shared with an unwanted, unlinked Chrome window gives no link;
  - created diff: one new window, zero new, two new, and a new window that is already linked is ignored.

Commit: `feat(browser): link browser windows to window-manager windows`

### Task 6: Placement and swap in `browser.Service`

**Files:** `internal/browser/service.go`, `module.go`, `placement_test.go`, `testing.go` (if needed).

**Constructor:** `NewService(driver Driver, store *Store, sessions Sessions, wmDriver wm.Driver, layout Layout)`, with `type Layout struct { BrowserWorkspace, ParkWorkspace string }`. Update `NewModule` to take the same extra parameters and pass them through. Update every existing test and caller (`newHarness`, `router_test`, `internal/api/app.go`) to pass `wm.None{}` and `Layout{"2", "P"}`, or a `wm.FakeDriver` where placement is tested.

**State:**
- `attached`, an `atomic.Uint64`, **not** guarded by `s.mu`: `Open` can hold `s.mu` for seconds, and `SetAttached` must never wait on it. Seed it when connected: in `resync`, if `attached == 0`, ask `Sessions.Attached(ctx)` and store it with `CompareAndSwap(0, id)`, so a concurrent `SetAttached` wins. `Sessions.Attached` is backed by `SessionService.AttachedSessionID`, a store query that selects only the attached session's ID.
- Add `Attached(ctx) (uint, error)` to the `Sessions` interface. It returns the currently attached session ID, or 0. Implement it in `internal/api/browsersessions.go` with `SessionService.AttachedSessionID`, a narrow store query (most recently used attached session). Add it to `FakeSessions`, backed by the `AttachedID` field.
- `linker` (Task 5).
- `swapCh chan struct{}`, a 1-slot channel.

**`SetAttached(sessionID uint)`:**
- Stores the ID atomically, without taking `s.mu`.
- Does a non-blocking send on `swapCh`.
- Never takes long, because it is called from synchronous eventbus handlers. The tuios hook's curl uses `--max-time 1`.

**Worker:**
- `OnAppStart` also starts `go s.placeLoop(ctx)`, which on every signal calls `s.place(ctx)`, logging errors with `slog.Warn`.

**`place(ctx)`, under `s.mu`:**
1. Return immediately if not connected.
2. `live := driver.ListWindows`.
3. `wmWins := wm.ListWindows(ctx, driver.AppBundleID())`.
4. `rows := store.ListOpen()`. Add `Store.ListOpen` if it doesn't exist, with a store test.
5. Resolve links for the Chrome IDs in `rows`.
6. For each linked row, the desired workspace is `BrowserWorkspace` when `row.SessionID == s.attached && s.attached != 0`, otherwise `ParkWorkspace`. If the wm window's current workspace differs, call `MoveToWorkspace`.
7. Never touch unowned windows. Never focus anything.

**Hooks:**
- Call `SetAttached` from an eventbus subscription to `SessionAttached`, registered in `NewModule`. Pass the bus to `NewModule`, following the `mux.NewModule(driver, bus, db)` pattern.
- Signal `swapCh` (without changing `attached`) at these points:
  - after `resync`, at the end of `handleEvent(EventConnected)`;
  - after `adopt` inserts a row;
  - on `EventTabsChanged`, because a title may now make a link possible. Only do this when that window is owned but not yet linked, so it doesn't churn.
- In `Open`'s create branch for a session (not session 0):
  1. take `before := wm.ListWindows` just before `CreateWindow`;
  2. after creating, poll `wm.ListWindows` every 100 ms for up to 1.5 s, until `linker.linkCreated` succeeds or time runs out;
  3. then signal `swapCh`. `Open` must not wait for the move itself.
  - Hold the lock as `Open` already does. The wm polling happens inside `Open`'s existing critical section, and that is acceptable.
- In `Open`, when `activated` is true, call `SetAttached(req.SessionID)` before taking the lock. The swap that follows then uses the new target, and is signalled, even if the eventbus event arrives late or never.
- In `Open`, when `activated` is true and an existing window is found, call `placeLocked(ctx)` (the body of `place`, without taking the lock) before `ActivateTab`/`OpenTab` and `FocusWindow`. Log errors; never fail `Open`.

**Tests** use `FakeDriver` + `wm.FakeDriver` + `FakeSessions` + `testdb`. Call `place` directly; it is synchronous.
- Swap: sessions 1 and 2 each own a linked window. With `SetAttached(1)` and `place`, 1's window is on `2` and 2's is on `P`. Switching to 2 reverses that.
- Unowned windows are never moved.
- A window already in the right place doesn't move (no recorded move).
- Ambiguous titles mean no move.
- A window created by `Open` for a background session gets linked through the created-diff and parked. Have the fake wm gain a window when the fake browser creates one, using a hook on `browser.FakeDriver` such as `OnCreate func(Window)`.
- `wm.None{}` leaves placement inert, and `Open` still works.
- `SetAttached` coalesces: several calls give at most one pending signal, and it never blocks.
- The placement loop runs `place` after a signal. Use `Eventually`.
- Seeding: when connected with `attached == 0`, `Sessions.Attached` seeds it.

Commit: `feat(browser): park inactive sessions' windows on the park workspace`

### Task 7: Daemon configuration and wiring

**Files:** `internal/api/config.go`, `internal/api/app.go`, `internal/api/browsersessions.go`, and the tests (`config_test.go`, `browsersessions_test.go`).

Add these flags, each with an environment variable that overrides it, following the existing pattern:

| Flag | Env | Default |
|---|---|---|
| `--wm` | `UTENA_WM` | `aerospace` |
| `--browser-workspace` | `UTENA_BROWSER_WORKSPACE` | `2` |
| `--park-workspace` | `UTENA_PARK_WORKSPACE` | `P` |

- `--wm` accepts only `aerospace` or `none`. `LoadConfig` returns an error for anything else.
- In `buildApp`, `wm == "none"` gives `wm.None{}` and anything else gives `aerospace.NewDriver()`. Only `internal/api` imports `internal/aerospace`.
- Pass the bus and `browser.Layout{...}` into `browser.NewModule`.
- Tests:
  - config defaults;
  - environment overrides;
  - an invalid `--wm` is rejected;
  - the wiring test still mounts `/browser`. `setupTestRouter` should not shell out to `aerospace`. Make sure the test app uses `wm.None{}`, e.g. by setting `cfg.WM = "none"` in the test helper's `Config`.

Commit: `feat(api): configure the window manager and browser workspaces`

### Task 8: Docs

**Files:** `CLAUDE.md`, `README.md`, and the spec.

- `CLAUDE.md`: add `internal/wm/` and `internal/aerospace/` to the module list. Extend the Browser paragraph with the `wm` seam, the fact that only `internal/api` imports `aerospace`, the swap worker, and `session.attached`.
- `README.md`: under "Browser windows", explain parking, the three flags, the `P` workspace and the `alt-b` / `alt-shift-b` bindings from dotfiles, and that `--wm none` turns parking off.
- Spec: add a "PR 2 amendments" section that lists the decisions in this plan's header.

Commit: `docs: browser parking with the wm driver`

### Task 9 (dotfiles repo, branch `eqt/utena-window-parking`): `P` bindings

**File:** `programs/aerospace/contents/aerospace.toml.tmpl` in `/Users/eleonora/utena-sessions/utena-window-management/dotfiles`.

- In **both** `[mode.main.binding]` and `[mode.term.binding]`, add:
  - `alt-b = [ 'mode main', 'workspace P' ]`
  - `alt-shift-b = 'move-node-to-workspace P'`

  Check that `alt-b` and `alt-shift-b` aren't already bound anywhere in the file.
- No `persistent-workspaces` entry and no monitor assignment. AeroSpace creates `P` on demand.
- Verify with `task build` in that repo (or the repo's `/task` equivalent). Read its `CLAUDE.md` for the rules; never run the shizuku binary directly.

Commit: `feat(aerospace): bindings to view and park windows on workspace P`

### Task 10: Smoke test

The controller does this, not a subagent. The daemon gets `task install`ed only after the user agrees.
- Switching sessions with the utena popup moves the windows quietly between `2` and `P`.
- Switching with Ctrl+B S in tuios does the same, through the `after-attach` hook.
- A background open parks the new window.
- `--wm none` turns parking off.
