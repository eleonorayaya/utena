# Browser window management

## Goal

Give every utena session its own browser windows. Links that come from a session
open in that session's windows. Switching sessions swaps which windows are visible
on the browser workspace. Archiving a session snapshots its tabs and closes them,
and they come back when the session is reactivated. The browser and the OS window
manager are each reached through a generic `Driver` interface, following the
`mux`/`tuios` split. Chrome and AeroSpace are the first drivers. Firefox is a
planned second browser driver, and the interface is shaped with it in mind.

## UX

- **One or more windows per session, created lazily.** A session gets its first
  window the first time something opens a link for it. Sessions that never browse
  never get a window.
- **Parking.** The attached session's windows live on the browser workspace (`2`).
  Every other session-owned window lives on the park workspace (`P`). Unowned
  windows (personal browsing) are never moved.
- **A quiet swap.** Changing the attached session, whether through the utena popup,
  Ctrl+B S or `utena attach`, swaps the windows but never changes focus. You stay in
  the terminal.
- **Link routing.** `utena shell-init` exports `BROWSER` pointing at `utena-open`,
  so `gh … --web` and other tools that respect `$BROWSER` open in the calling
  session's window (from `UTENA_SESSION_ID`). The TUI's `o` (open PR) uses the same
  path.
  - From the attached session: the link opens and AeroSpace focuses that window,
    which jumps to workspace 2.
  - From a background session: the link opens quietly in the parked window, with
    no focus change.
  - From outside any session: plain `/usr/bin/open`.
  - `o`, or selecting a tab, on a session that isn't attached switches tuios to
    that session and then opens it as the attached session. This is one daemon
    call.
- **Dedup by prefix.** If an open tab has the same host as the requested URL and its
  path starts with the requested path (whole segments only, ignoring query and
  fragment), that tab is focused as it is and not navigated. Otherwise a new tab
  opens. `/pull/123` focuses `/pull/123/files`. `/pull/12` does not match
  `/pull/123`.
- **The link target** for a session with several windows is its most recently
  focused open window.
- **Ownership of new windows.** A window created while one of session S's windows
  is focused (cmd-N, or dragging a tab out) belongs to S. A window created from an
  unowned window stays unowned.
- **Lifecycle.**
  - Archive: snapshot every window's tabs, then close the windows.
  - Delete: close the windows and drop their snapshots.
  - Closing a window by hand while Chrome is running: that window is forgotten and
    nothing is restored.
  - Chrome quits or crashes: on restart, utena reattaches restored windows by URL
    overlap. Windows that don't match become `missing` and are restored from their
    snapshot.
  - Restoring `missing` and `snapshotted` windows is lazy: it happens when the
    session becomes attached, or when a link is opened for it.
- **TUI.**
  - The session list shows a marker for sessions with browser windows.
  - The detail panel shows the tab count, the window state, and a tab list grouped
    by window. A heading appears only when there are several windows.
  - Selecting a tab activates it and focuses its window, switching sessions first
    if needed.
  - The footer warns when the extension is outdated.
- **Graceful degradation.** `--wm none` keeps routing, tab lists and snapshots but
  turns parking off. With no extension connected, `utena-open` falls back to
  `/usr/bin/open`.

## Packages

```
internal/browser    domain: BrowserWindow model, store, service, /browser controller, Driver interface, FakeDriver
internal/chrome     browser.Driver over a WebSocket to the Chrome extension; exposes Handler()
internal/wm         wm.Driver interface, types, FakeDriver, a no-op driver
internal/aerospace  wm.Driver over the aerospace CLI
extension/chrome    MV3 extension (manifest with a pinned key, background.js)
```

Only `internal/api/app.go` imports `chrome` and `aerospace`. `browser` depends on
`wm`, `mux` and the eventbus, never on a concrete driver. `app.go` mounts
`chrome.Driver.Handler()` at `/browser/chrome`.

```go
package browser

type Driver interface {
	AppBundleID() string
	ListWindows(ctx context.Context) ([]Window, error)
	CreateWindow(ctx context.Context, urls []string, focused bool) (Window, error)
	CloseWindow(ctx context.Context, windowID string) error
	OpenTab(ctx context.Context, windowID, url string) (Tab, error)
	ActivateTab(ctx context.Context, windowID, tabID string) error
	Subscribe(ctx context.Context, onEvent func(Event)) error
}

type Window struct {
	ID      string
	Focused bool
	Tabs    []Tab
}

type Tab struct {
	ID, URL, Title string
	Active, Pinned bool
}

type EventType string

const (
	EventConnected     EventType = "connected"
	EventDisconnected  EventType = "disconnected"
	EventWindowCreated EventType = "window_created"
	EventWindowRemoved EventType = "window_removed"
	EventTabsChanged   EventType = "tabs_changed"
	EventFocusChanged  EventType = "focus_changed"
)

type Event struct {
	Type     EventType
	WindowID string
}
```

```go
package wm

type Driver interface {
	ListWindows(bundleID string) ([]Window, error)
	MoveToWorkspace(windowID, workspace string) error
	FocusWindow(windowID string) error
}

type Window struct {
	ID, Title, Workspace string
}
```

Per-tab process attribution for `utena top` is out of scope. Stable Chrome has no
API that maps tabs to process IDs. It is recorded as future work and is not part
of the interface.

## Data model

```go
type WindowState string

const (
	WindowOpen        WindowState = "open"
	WindowMissing     WindowState = "missing"
	WindowSnapshotted WindowState = "snapshotted"
)

type BrowserWindow struct { // table browser_windows
	gorm.Model
	SessionID     uint        `gorm:"index"`
	State         WindowState
	Tabs          []SavedTab  `gorm:"serializer:json"`
	LastFocusedAt time.Time
	SnapshotAt    time.Time
}

type SavedTab struct {
	URL, Title     string
	Active, Pinned bool
}
```

- Having no row means no window: the session never needed one, or you closed it.
- Whether a window is parked isn't stored. It follows from comparing the window's
  session with the attached session.
- Chrome window IDs and AeroSpace window IDs are kept only in memory:
  `rowID → {chromeWindowID, wmWindowID}`. They are rebuilt on every `Connected`.
- `Tabs` is a rolling snapshot, rewritten on `TabsChanged`, debounced by about 2
  seconds.
- Reattach: each live Chrome window is matched to the `open` or `missing` row with
  the highest Jaccard overlap of URLs, if it reaches a threshold. Each window and
  each row is used at most once. Ties stay unowned.

## Event flow

| Trigger | Source | Action |
|---|---|---|
| The attached session changes | `mux.client_session_changed`, `mux.client_attached` | Move the old session's windows to `P` and the new session's to `2`. Restore `missing`/`snapshotted` windows. Don't change focus |
| A session is archived | new `session.archived` event | Snapshot and close all of its windows. Rows become `snapshotted` |
| A session is deleted | new `session.deleted` event | Close the windows and delete the rows |
| `Connected` | the extension's `hello` | Resync: list windows, reattach, link AeroSpace IDs, re-run the swap |
| `WindowCreated` | extension | Inherit an owner from the previously focused window, link the AeroSpace ID, park the window if needed |
| `WindowRemoved` | extension | Wait about 2 seconds. A disconnect in that window means Chrome quit, so mark `missing`. Otherwise it was a deliberate close, so delete the row |
| `TabsChanged` / `FocusChanged` | extension | Debounced snapshot, and update `LastFocusedAt` |

### `POST /browser/open {session_id?, url, activate?}`

1. If `activate` is set, activate the session through the existing session/mux
   path and run the swap right away. Don't wait for the tuios client to reattach.
2. Find the target window: the most recently focused `open` window. If there is
   none, restore `missing`/`snapshotted` windows. If there are still none,
   `CreateWindow`.
3. Look for a prefix-dedup match. Use `ActivateTab` if there is one, otherwise
   `OpenTab`.
4. If the session is attached, call `wm.FocusWindow`. Otherwise do nothing else.

Returns `503` when no browser driver is connected.

### Linking a Chrome window to an AeroSpace window

An AeroSpace window's title is the active tab's title, with no browser suffix.
This was checked against a live window. Matching works like this:

1. Compare the active tab's title with `aerospace list-windows` titles, restricted
   to the driver's bundle ID.
2. If the titles are ambiguous, open a short-lived marker tab
   (`data:text/html,<title>utena:<nonce></title>`) in that window, read the
   AeroSpace ID, and close the marker tab.

Links are made on `WindowCreated` and on reattach. AeroSpace IDs stay stable for a
window's lifetime.

### `utena-open` and `$BROWSER`

- `tui:install` creates the symlink `~/.local/bin/utena-open → utena`. `utena`
  handles `argv[0] == "utena-open"` as `utena open <url>`.
- `shell-init` exports `BROWSER=$HOME/.local/bin/utena-open`. The path has no
  spaces, which is safer for tools that don't split `$BROWSER` with shlex.
- The TUI's `ui.OpenURL` calls the daemon, sending `activate` for a session that
  isn't attached.
- If the daemon can't be reached or returns `503`, it falls back to
  `/usr/bin/open <url>`, which ignores `$BROWSER`, so it can't loop.

## Extension

- The source is `extension/chrome/`, an MV3 extension using the `tabs`, `windows`
  and `alarms` APIs. A pinned `key` keeps its ID stable. It doesn't depend on
  Chrome-only APIs, so a Firefox build can reuse it with its own manifest and a
  `/browser/firefox` driver.
- The protocol is JSON over `ws://127.0.0.1:3333/browser/chrome`:
  - `{"type":"hello","protocol":1,"browser":"chrome"}`
  - `{"type":"req","id":7,"method":"tabs.open","params":{…}}` and
    `{"type":"res","id":7,"result":{…}}` or `"error"`
  - `{"type":"event","event":"window.removed","data":{…}}`
- On a protocol mismatch, the driver reports itself unavailable, logs a warning,
  and the TUI footer shows "browser extension outdated, reload it".
- Keepalive: a ping every 20 seconds. Reconnect backoff runs from 1 to 30 seconds.
  A `chrome.alarms` alarm every 30 seconds and `runtime.onStartup` wake a
  suspended service worker so it can reconnect.
- Install: `task extension:install` copies the extension to
  `~/.local/share/utena/chrome-extension`. Then, once: in `chrome://extensions`,
  enable Developer mode and use Load unpacked.

## Configuration

Daemon flags, each with an env var override in the style of `--branch-prefix`:

| Flag | Default | Meaning |
|---|---|---|
| `--browser` | `chrome` | Which `browser.Driver` to build |
| `--wm` | `aerospace` | Which `wm.Driver` to build. `none` turns parking off |
| `--browser-workspace` | `2` | Where the attached session's windows live |
| `--park-workspace` | `P` | Where all other session-owned windows live |

## Dotfiles (companion change)

- `programs/aerospace/contents/aerospace.toml.tmpl`:
  - add `persistent-workspaces` including `P`;
  - bind `alt-b` = `workspace P` and `alt-shift-b` =
    `move-node-to-workspace P` in both the `main` and `term` modes;
  - keep the Chrome `on-window-detected` rule limited to startup, so utena does
    all other placement.

## Risks, settled in a spike at the start of PR 1

1. **Linking.** Is matching by title, with the marker tab as a fallback, reliable?
   Does the marker tab stay invisible enough?
2. **Telling a quit from a close.** Does Chrome fire `windows.onRemoved` for every
   window before the socket drops when it quits? Tune the grace period for the
   "removed, then disconnected" signal.
3. **Background windows flashing.** A window created for a background session may
   appear on the focused workspace for about 200 ms before it is parked. Try
   `state: "minimized"`, then move it to `P` and restore it. If that doesn't work,
   accept the flash.

### PR 1 smoke findings (2026-10-15)

- The AeroSpace window title is `<active tab title> - Google Chrome`. Title matching
  must strip the ` - Google Chrome` suffix. An earlier sample without the suffix was
  misleading.
- AeroSpace followed Chrome's native focus here: `chrome.windows.update({focused: true})`
  on a window in workspace `2` switched AeroSpace to `2`. PR 2 live testing showed
  this is unreliable: macOS cooperative app activation stops Chrome from activating
  itself while another app (the terminal) is frontmost, so the user stays on the
  terminal workspace. Focus therefore goes through `wm.FocusWindow` (see PR 2
  amendments), which supersedes PR 1 amendment 1.
- A window created with `focused: false` for a background session does not take
  focus. AeroSpace places it on the focused workspace (`T`) unless an
  `on-window-detected` rule moves it. The dotfiles rule now sends every new Chrome
  window to `2`, with no focus-follow. PR 2 still has to move background-session
  windows to `P`.
- The IDs Chrome's AppleScript uses for windows (`window id N`) are the same as the
  extension's window IDs. That is a usable debugging and fallback path.
- These were confirmed live:
  - prefix dedup reuses an existing tab;
  - a different URL opens a new tab;
  - a background session never steals focus;
  - a session-0 link goes to an unowned window and focuses it;
  - a window created from a session window is adopted by that session, and closing
    it deletes its row.
- Still open:
  - quit versus close on cmd-Q (risk 2);
  - minimized creation to avoid the flash (risk 3);
  - marker-tab linking (risk 1). These carry over to PRs 2 and 3.

## Testing

- `browser` service tests run against `browser.FakeDriver` and `wm.FakeDriver`
  with in-memory SQLite:
  - the swap on attach changes;
  - prefix dedup (a table test, including the segment boundary);
  - inheriting ownership from the focused window;
  - quit versus close, using the grace period with an injectable clock;
  - reattach matching, including ties;
  - lazy restore;
  - archive and delete;
  - the decision whether to focus on `open`.
- `chrome` driver tests run against an in-process WebSocket test client playing
  the extension:
  - request and response correlation;
  - mapping events to `browser.Event`;
  - rejecting a protocol mismatch;
  - a disconnect producing `Disconnected`.
- `aerospace` tests use a fake runner and parse real `list-windows --json`
  output.
- `utena-open` tests cover the fallback to `/usr/bin/open` on connection refused
  and on `503`.
- TUI tests cover the tab list rendering and the `o` and tab-select messages.
- `task test` and `task lint` must pass. Do a manual smoke test with the real
  Chrome and AeroSpace before each PR merges.

## Delivery

1. **The browser core.** The spike, `browser`, `chrome`, the extension,
   `/browser/open`, `utena-open`, `$BROWSER` in `shell-init`, and the TUI `o`
   rerouted. All of it works with `--wm none`.
2. **Parking.** `wm` and `aerospace`, linking, the swap on attach change, and the
   dotfiles AeroSpace change.
3. **Lifecycle.** Snapshots, archive and delete, telling a quit from a close,
   reattach, and lazy restore.
4. **TUI.** The list marker, the detail panel's tab list, tab select, and the
   footer warning for an outdated extension.

## Out of scope

- A Firefox driver. The interface allows for one.
- Mapping tabs to processes in `utena top`.
- Adopting or releasing windows from the TUI, and closing tabs from the TUI.
- Taking over as the system-wide default browser, and kitty cmd-click routing.
- Seed tabs on session create.

## PR 1 amendments

The browser core departs from this design in a few small ways.

From the implementation plan:

- `browser.Driver` gains `FocusWindow`, which sets Chrome's own active window. Superseded in PR 2: it alone does not bring the window forward, and `Open` also focuses through `wm.FocusWindow`.
- Rows store `DriverWindowID` as a hint, so a daemon restart while Chrome keeps running keeps ownership. Rows whose window is gone on `Connected` become `missing`, with the ID cleared. Restoring them is PR 3.
- Links from outside any session (`session_id` 0) go to an unowned Chrome window through the daemon, not straight to `/usr/bin/open`. That keeps personal links out of session windows.
- There's no `--browser` flag yet. Chrome is the only driver, and with no extension connected the daemon returns 503, which triggers the fallback.
- The extension has no pinned `key`. Without a native host, nothing needs a fixed extension ID. The WebSocket endpoint accepts only `chrome-extension://` origins, which web pages cannot forge.

Decided during review:

- The daemon binds `127.0.0.1` (it used to bind all interfaces), and every utena client (CLI, TUI, the `shell-init` curl, the Claude hook and the extension) uses `127.0.0.1`. The `/browser/chrome` websocket must not be reachable from the LAN, and `[::1]` must not be squattable. The dotfiles still use `localhost` in `programs/sketchybar/bar/utena.go` and in the tuios `after-attach` hook curl. That works through the fallback and switches over in the dotfiles change (PR 2).
- `browser.Service` keeps the windows it creates for session-0 links unowned; the focused session never adopts them. `ErrNotConnected` from the driver surfaces as `ErrUnavailable` (503). A resync resets focus tracking.
- The Chrome driver coalesces connection-state events and never drops them. Window and tab events are tagged with the connection generation, and stale ones are discarded.
- The extension does not reconnect on its own after a `"replaced"` close, but the 30s alarm still reconnects. Two profiles with the extension will therefore swap ownership roughly every 30s, so install it in one profile only.
- Known edge: a slow Chrome that completes after the daemon's 15s (or the driver's 5s) timeout can lead to a duplicate open through the `/usr/bin/open` fallback. Accepted.
- Known edge: with `activate`, a driver failure after the session switch returns an error, so the TUI falls back to `/usr/bin/open` but does not quit; the popup stays open in the previous session. Accepted.
- The extension drops (does not queue) events sent before its hello; the resync on `Connected` covers them.
- The extension's endpoint is hardcoded to port 3333, so dev daemons on other ports always fall back.
- The `--browser` flag is deferred until there is a second driver.
- Root URLs (no path, such as `https://example.com/`) never dedup. Every open of one gets a new tab.
- Chrome window IDs restart with each browser run. The extension keeps a per-run ID in `chrome.storage.session` and sends it in `hello` as `run`. The daemon persists the last run ID in a single-row `browser_states` table. On `Connected` with a non-empty run that differs from the stored one, every open row becomes `missing` before the usual resync, so a reused window ID never inherits an old owner. `storage.session` is also cleared when the extension is reloaded or updated, which forgets ownership even though Chrome kept its windows. That fails safe. An extension that sends no `run` keeps the old behavior. The protocol version doesn't change.
- `/browser/open` and `/browser/status` reject any request that carries an `Origin` header with 403. Utena's own clients never send one, and this stops web pages from posting to the daemon. The `/browser/chrome` websocket keeps its `chrome-extension://` origin check instead.
- An unknown `session_id` falls back to an unowned window, the same as `session_id` 0.
- Archived sessions can still get a new window. `missing` rows aren't cleaned up until PR 3.

## PR 2 amendments

Parking departs from this design in these ways (see `docs/superpowers/plans/2026-10-15-browser-parking.md`):

- `wm.Driver` has `ListWindows(ctx, bundleID)`, `MoveToWorkspace(ctx, windowID, workspace)` and `FocusWindow(ctx, windowID)`. Live testing showed Chrome cannot reliably activate itself while another app is frontmost (macOS cooperative app activation), so relying on AeroSpace to follow Chrome's native focus left the user on the terminal workspace. Whenever `Open` intends to focus (attached session, activated session, session 0), it calls `browser.Driver.FocusWindow` (so Chrome's own window order and key window are right) and then focuses the window explicitly with `wm.FocusWindow` (AeroSpace `focus --window-id`), which switches to the window's workspace. The wm window is found with the same linking rules as placement, resolved once at focus time (cached link, created-window diff, or a unique exact title among unclaimed wm windows; never a guess). If none is found, or the wm call fails, `Open` logs at warn and still succeeds. A created window is focused only through its created-window diff link; the title match is skipped there because the Chrome window list is stale. Windows created for session-0 links are linked by the created-window diff too, so they can be focused; they stay unowned and are never moved. A window is never focused on the park workspace: if the wm window to focus is on `P`, `Open` first moves it to the browser workspace, and does not focus it if that move fails. `Open` decides whether to focus from the session store, but placement uses the attached ID held by `browser.Service`, which can lag the `session.attached` event. So when `Open` will focus a session other than the one placement last saw, it calls `SetAttached` and runs placement synchronously first, the same way an activating `Open` does. The created-window poll stops on the first `wm.ListWindows` error instead of retrying until the deadline. `wm.None` (`--wm none`) is inert: placement is skipped entirely and focus is browser-only.
- `browser.Driver` gained `AppBundleID()` and `WindowTitle(Window) string`, the title the OS shows for a window (for Chrome, `"<active tab title> - Google Chrome"`). The title format stays inside the driver.
- The swap trigger is a new `session.attached` eventbus event, published by `SessionService` after every store update that sets `IsAttached`. `browser.Service.SetAttached` stores the ID in an `atomic.Uint64` (never under the service lock, since `Open` can hold it for seconds) and signals a 1-slot channel. A single worker runs placement and coalesces signals. On `Connected` the attached ID is seeded from the session store with a compare-and-swap, so a concurrent `SetAttached` wins.
- Detach never clears the attached session. There is no `SessionAttached{0}`; the last attached session's windows stay on the browser workspace.
- Linking order replaces §Linking:
  1. a cached link that is still live;
  2. for windows utena creates, the one AeroSpace window that appeared since a snapshot taken just before `CreateWindow`, polled for up to 1.5 s, ignoring wm IDs already linked. The diff is also checked on the Chrome side: if any other Chrome window appeared in the meantime, the link is dropped, because the new wm window may be that one's;
  3. an exact title match among unlinked wm windows, only when no other unlinked Chrome window (wanted or not) has the same title.
- There is no elimination step. AeroSpace lists Chrome windows the extension never reports (popups, picture-in-picture, undocked DevTools, other profiles), and AeroSpace can lag behind the extension, so "the last unlinked window" may be a foreign one.
- An ambiguous window is not moved. It is logged at debug level and retried on the next `TabsChanged` for that window (only while it is owned but unlinked) or the next swap. The marker-tab fallback is deferred until real ambiguity shows up.
- An activating `Open` that reuses an existing window runs placement synchronously, under its lock, before it changes tabs or focuses. Focusing a window still parked on `P` would otherwise pull the user's view to `P`, and placing first keeps the window's title matching its wm title. The wm window to focus is resolved before the tab change for the same reason.
- Placement moves only session-owned windows that are on the wrong workspace, never focuses anything, and never moves unowned windows. Swaps and background-session opens never focus; only `Open` focuses, as above.
- Creating windows minimized is not attempted (risk 3). A background-session window may briefly appear on `2`, where the dotfiles `on-window-detected` rule puts it, before it is moved to `P`.
- Dotfiles: `alt-b` (`workspace P`) and `alt-shift-b` (`move-node-to-workspace P`) in both `main` and `term` modes. No `persistent-workspaces` entry; AeroSpace creates `P` on demand.
- The flags are `--wm aerospace|none` (`UTENA_WM`), `--browser-workspace` (`UTENA_BROWSER_WORKSPACE`, default `2`) and `--park-workspace` (`UTENA_PARK_WORKSPACE`, default `P`). Any other `--wm` value is a config error. Only `internal/api` imports `internal/aerospace`.
