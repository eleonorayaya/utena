# Browser Lifecycle (PR 3) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** A session's browser windows survive archive, Chrome quits, Chrome restarts and daemon restarts.

- **Archiving** snapshots each window's tabs, then closes it.
- **Reactivating** the session restores its windows lazily.
- **Quitting Chrome** marks windows `missing`, keeping their tabs.
- **Restarting Chrome** reattaches each restored window to its session by URL overlap. Windows with no match are restored from their snapshot the next time they're needed.
- **Closing a window by hand** while Chrome is running still forgets it.
- **Deleting a session** closes its windows and drops their rows.

**Stacks on:** PR 2 (`eqt/utena-window-parking`, utena#125). This branch is `eqt/utena-window-lifecycle`.

**Spec:** `docs/superpowers/specs/2026-10-15-browser-window-management-design.md`. The relevant parts are §UX "Lifecycle", §Data model, §Event flow, "PR 1 amendments" and "PR 2 amendments".

**Architecture:**
- `BrowserWindow` gains:
  - `Tabs []SavedTab` (JSON serializer), a rolling snapshot debounced on `TabsChanged`;
  - `SnapshotAt time.Time`;
  - a third state, `snapshotted`.
- `browser.Driver` gains `CloseWindow`. The extension adds `windows.remove`, and the protocol is bumped to `2` so a stale extension is reported as outdated.
- Session archive and delete publish new eventbus events, which `browser.Service` handles.
- **Quit vs close.** A `WindowRemoved` is held as pending for a short grace period. If `Disconnected` arrives during the grace period, Chrome quit, so the row becomes `missing` and keeps its tabs. If the grace period expires first, the user closed the window, so the row is deleted. This replaces PR 1's "delete immediately".
- **Reattach on `Connected`.** Each live Chrome window that no current row owns is matched to a `missing` row by Jaccard overlap of normalized tab URLs. The match must reach a threshold, the best match wins, ties stay unowned, and each window and each row is used at most once.
- **Lazy restore.** When a session becomes attached, or `Open` targets a session with no live window, its `missing` and `snapshotted` rows are recreated with `CreateWindow(urls)`. The previously active tab is reactivated. Restored windows go through the same created-diff link and placement as PR 2.

**Rules** (same as PRs 1 and 2):
- Run `task` from the repo root only: `task test --force` and `task fmt`. Never run `go` directly, never `cd` into subdirectories, and never run install tasks.
- Lint by inspection; `golangci-lint` isn't available.
- No code comments.
- Work TDD.
- One commit per task. End every message with a blank line and then `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
- Known pre-existing failure: `internal/session` `TestSessionService_AddWorkspace_FullIntegration`.
- Never shell out to real `aerospace` or Chrome in tests.

---

### Task 1: Snapshot data model

**Files:** `internal/browser/window.go`, `store.go`, `store_test.go`.

- Add `type SavedTab struct { URL, Title string; Active, Pinned bool }` with JSON tags.
- On `BrowserWindow`, add `Tabs []SavedTab` with `gorm:"serializer:json"` and `SnapshotAt time.Time`.
- Add the constant `WindowSnapshotted WindowState = "snapshotted"`.
- Store methods:
  - `SaveTabs(driverWindowID string, tabs []SavedTab, at time.Time) error` updates the open row.
  - `ListBySession(sessionID uint) ([]BrowserWindow, error)` returns all non-deleted rows in any state.
  - `ListRestorable(sessionID uint) ([]BrowserWindow, error)` returns `missing` and `snapshotted` rows, newest `last_focused_at` first.
  - `MarkSnapshotted(id uint, tabs []SavedTab, at time.Time) error` sets state `snapshotted`, clears `driver_window_id`, and stores the tabs.
  - `Reopen(id uint, driverWindowID string, at time.Time) error` sets state `open`, the new ID and `last_focused_at`.
  - `MarkMissingByDriverWindowID(id string) error` sets state `missing` and clears the ID, keeping the tabs.
  - `DeleteBySession(sessionID uint) error`.
  - `ListMissing() ([]BrowserWindow, error)`.
- `MarkMissingExcept` must keep `Tabs`. Add a test that proves it.
- Add a helper `func savedTabs(w Window) []SavedTab` in `window.go`. It skips `chrome://newtab` and empty URLs.
- Tests: a JSON round trip through the store, plus each method.

Commit: `feat(browser): persist a snapshot of each window's tabs`

### Task 2: `CloseWindow` and protocol 2

**Files:**
- `internal/browser/driver.go`
- `internal/browser/testing.go`
- `internal/chrome/driver.go`
- `internal/chrome/protocol.go`
- `internal/chrome/driver_test.go`
- `extension/chrome/background.js`
- `extension/chrome/manifest.json`

**Changes:**
- Add `CloseWindow(ctx context.Context, windowID string) error` to `browser.Driver`.
  - **Chrome:** `call("windows.remove", {windowId})`.
  - **Fake:** removes the window and records it in `Closed() []string`, with an optional `OnClose` hook.
- **Extension:**
  - Add `"windows.remove": async ({windowId}) => { await chrome.windows.remove(windowId); return {}; }`.
  - Bump `PROTOCOL` to `2`, and the manifest `version` to `0.2.0`.
- Bump the driver's `ProtocolVersion` to `2`.
- **Tests:** a driver test for `windows.remove` params; the protocol-mismatch test still passes with version 1 being rejected.
- Run `node --check` on the extension.

Commit: `feat(browser): close windows through the driver (protocol 2)`

### Task 3: Rolling snapshot

**Files:** `internal/browser/service.go`, `lifecycle_test.go`.

- On `EventTabsChanged` for an owned window, schedule a snapshot of that window ID, debounced at about 2 s and coalesced per window.
  - Use a small `debouncer` keyed by window ID, built on an injectable `afterFunc func(d time.Duration, f func()) (stop func())` that defaults to `time.AfterFunc`, so tests can fire it by hand.
- When the snapshot fires:
  - Take `s.mu` and return early if disconnected.
  - `ListWindows`, find the window, and call `store.SaveTabs(id, savedTabs(w), now)` if it is still owned.
- Also snapshot every owned live window during `resync` on `Connected`, and in `adopt` and `Open`'s create branch (the initial tabs).
- **Tests:**
  - A `TabsChanged` burst produces one save carrying the latest tabs.
  - Unowned windows are never saved.
  - A disconnected service doesn't save.
  - `resync` saves owned windows.

Commit: `feat(browser): keep a rolling snapshot of session windows`

### Task 4: Telling a quit from a close

**Files:** `internal/browser/service.go`, `lifecycle_test.go`.

- Constant `removeGrace = 2 * time.Second`, set as a field so tests can change it, and using the same injectable `afterFunc`.
- **`EventWindowRemoved` for an owned window:**
  - Don't delete. Record `pendingRemoval[windowID]` and start the grace timer.
  - Remove the window from the linker cache.
- **When the timer fires:** take `s.mu`. If the ID is still pending and the service is still connected, `DeleteByDriverWindowID` and clear the pending entry.
- **`EventDisconnected`:** for every pending ID, `MarkMissingByDriverWindowID`. Stop the timers and clear the pending set.
  - Also on disconnect: open rows stay `open`. The next `Connected` resync and reattach take care of them, because PR 1's run-id logic and `MarkMissingExcept` already mark them missing when their windows aren't live.
- **Tests:**
  - Removed, then the timer fires: the row is deleted.
  - Removed, then disconnected inside the grace period: the row is `missing` and its tabs are kept.
  - Removed for an unowned window: no-op.
  - A second `Connected` doesn't resurrect deleted rows.

Commit: `feat(browser): tell a Chrome quit from a closed window`

### Task 5: Reattach after a Chrome restart

**Files:** `internal/browser/reattach.go`, `reattach_test.go`, `service.go`.

- A pure function: `func matchRestored(rows []BrowserWindow, windows []Window, owned map[string]bool, threshold float64) map[string]uint`. It maps a Chrome window ID to a row ID.
  - Normalize URLs before comparing: scheme + host + path with the trailing slash trimmed, and drop the query, the fragment and new-tab pages.
  - Score = |A∩B| / |A∪B| over URL sets.
  - Consider only windows not in `owned` and rows with a non-empty `Tabs`.
  - Greedy, highest score first. Skip any pair whose score is below `threshold` (0.5).
  - **Tie rule:** if the top score for a window is shared by two rows, or the top score for a row is shared by two windows, leave both unmatched.
  - Each window and each row is used at most once.
- In `handleEvent(EventConnected)`, after `trackRun` and `resync`:
  - List the `missing` rows and run `matchRestored` against the live windows not owned by an open row.
  - For each match, `store.Reopen(row.ID, windowID, now)`.
  - Then signal placement, which parks restored windows of non-attached sessions.
- **Tests:**
  - an exact match;
  - a partial match above the threshold;
  - below the threshold;
  - tie on the window side;
  - tie on the row side;
  - owned windows excluded;
  - new-tab-only windows don't match;
  - **integration:** quit (removed, then disconnected), a new run id, then `Connected` with the same URLs: the rows are reattached as `open` with the new IDs.

Commit: `feat(browser): reattach restored windows by URL overlap`

### Task 6: Archive, delete and lazy restore

**Files:**
- `internal/eventbus/events.go`
- `internal/session/sessionservice.go` and its tests
- `internal/browser/service.go`, `module.go`, `lifecycle_test.go`

**Events:**
- Add `SessionArchived = "session.archived"`, `SessionDeleted = "session.deleted"` and `SessionLifecycleEvent{SessionID uint}`.
- `SessionService.ArchiveSession` publishes `SessionArchived` after the store update.
- `DeleteSession` publishes `SessionDeleted` after a successful delete.
- Publish errors are logged, not returned, the same as `publishSessionAttached`.
- Add tests next to the existing `sessionservice_attached_test.go` pattern.

**Browser handlers:**
- `NewModule` subscribes to both events. The handlers must not block the publisher for long, so they hand off to a goroutine that runs under the module ctx. Use a small work channel drained by the placement worker, or `go` with the module ctx; keep it simple.
- **Archive(sessionID):**
  - If connected: for each open row, `ListWindows`, snapshot its tabs, `MarkSnapshotted`, then `CloseWindow`. Add the window to a `closing` set so the resulting `WindowRemoved` is ignored and not treated as a user close.
  - If not connected: mark the open rows `snapshotted` with their last saved tabs.
- **Delete(sessionID):** `CloseWindow` every open window (connected only, same `closing` set), then `DeleteBySession`.

**Lazy restore:**
- Add a `restoreLocked(ctx, sessionID) ([]Window, error)` that runs under `s.mu`. For each restorable row:
  1. `urls := tab URLs`, falling back to `chrome://newtab` when empty.
  2. Take a wm snapshot.
  3. `CreateWindow(urls, false)`.
  4. Link it through the created-diff (the PR 2 helper).
  5. `ActivateTab` on the saved active tab's index, matched by position in `w.Tabs`.
  6. `store.Reopen`.
  7. Snapshot.
- Call it from two places:
  - **`placeLocked`**, before placing, for the attached session. It runs only when that session has no live open window and has restorable rows, and only when connected. Restored windows then get placed on the browser workspace.
  - **`Open` when `!found`** for a session: restore first. If anything was restored, the target is the most recently focused restored window: run `MatchTab` / `OpenTab` against it and don't create a new window.
- **Tests:**
  - Archive snapshots, closes and doesn't delete the row.
  - Archive's own `WindowRemoved` doesn't delete or mark anything.
  - Delete closes and drops the rows.
  - Attaching an archived session restores its windows with the saved URLs and active tab, and places them.
  - `Open` for a session with only a `snapshotted` row restores it and opens the link in it (dedup applies).
  - Restore while disconnected is a no-op.
  - Session-service event tests.

Commit: `feat(browser): snapshot on archive, close on delete, restore lazily`

### Task 7: Docs

- Update `CLAUDE.md` (the Browser paragraph: lifecycle, protocol 2) and `README.md` (archive/restore behaviour, reloading the extension after upgrading for protocol 2).
- Add a "PR 3 amendments" section to the spec covering:
  - the grace-period rule;
  - the Jaccard threshold of 0.5 and the tie rule;
  - lazy restore points;
  - restore doesn't restore pinned state;
  - `chrome://newtab` filtered from snapshots;
  - protocol 2.

Commit: `docs: browser window lifecycle`

### Task 8: Smoke test

The controller runs this after the user agrees: `task install`, then reload the extension.
- Archive a session: its windows close, and its rows are `snapshotted` with tabs.
- Reactivate it: the windows come back with the same tabs and are placed.
- Close a window by hand: its row is gone.
- cmd-Q Chrome and reopen it (needs the user's OK, because it closes their browser): windows are reattached, or restored when needed.
