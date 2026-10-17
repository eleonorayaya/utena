# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Utena is a workspace management system for tuios, consisting of two interconnected components:
- **daemon**: HTTP API server (Go) that manages workspace state and session information
- **tui**: Terminal UI client (Go + Bubbletea) for interacting with the daemon

The daemon drives tuios over its JSON control socket and follows the tuios event stream to track session and window state. The TUI queries the daemon to display workspace information and is launched from a tuios popup keybinding.

## Build & Run Commands

This project uses [Task](https://taskfile.dev) as its build system. NEVER run `go`, `cargo`, or other build commands directly. NEVER `cd` into subdirectories to run commands. ALWAYS use `task <target>` from the project root.

See the `taskfile-commands` skill for the full command reference.

Key commands: `task daemon:run`, `task tui:run`, `task fmt`, `task test`.

## Architecture

### Component Communication

1. **tuios → Daemon**: `mux.Service` holds a long-lived `Driver.Subscribe`; the tuios driver translates the tuios event stream into `mux.Event`s. Session created/closed and window events update `mux.Session` state and are republished on the eventbus (`mux.*` events). Every (re)subscribe emits `Resync`, which reloads from `ListSessions`/`ListWindows`.

2. **TUI → Daemon**: The TUI makes HTTP requests to `http://127.0.0.1:3333/sessions`. Activating a session publishes `SessionActivated`; `utena attach` long-polls `GET /mux/activations/next` and restarts its `tuios attach` child on the new session, reporting attach/detach via `PUT /mux/hooks/{event}`

3. **Daemon API**: Uses chi router, serves on port 3333 and listens on 127.0.0.1 only (clients must use `127.0.0.1`, not `localhost`), mounts controllers:
   - `/sessions` - session management endpoints (`GET /sessions/resources` backs `utena top`)
   - `/mux` - client attach hooks, activation long-poll, session env lookup
   - `/claude` - Claude session endpoints
   - `/workspaces` - workspace management endpoints
   - `/browser` - open links in session windows (`POST /open`), driver status, per-session window and tab state for the TUI (`GET /sessions`), tab focus that activates the session first if needed (`POST /focus`), and the extension websocket at `/browser/<driver name>` (`/browser/chrome` or `/browser/firefox`)

### Go Module Structure

- `cmd/daemon/main.go` - daemon entry point
- `cmd/tui/main.go` - TUI entry point
- `internal/api/` - HTTP API and daemon server
- `internal/db/` - SQLite database via GORM (Database interface, migrations)
- `internal/session/` - session controller logic
- `internal/workspace/` - workspace discovery and management
- `internal/mux/` - multiplexer-agnostic session domain: `mux.Session` model, store, service, `/mux` controller, `Driver` interface
- `internal/tuios/` - `mux.Driver` over the tuios control socket, agent-pane tagging, tuios workspace CLI helpers
- `internal/browser/` - browser-agnostic domain: session→window ownership, open/dedup/focus rules, `/browser` controller, `Driver` interface
- `internal/webext/` - shared `browser.Driver` over a websocket to the utena WebExtension (`extension/background.js`), parametrized by a `webext.Browser` (name, app bundle ID, extension origin scheme, wm window-title suffix, outdated hint)
- `internal/chrome/`, `internal/firefox/` - the Chrome (`extension/chrome`, MV3) and Firefox (`extension/firefox`, MV2 persistent background, signed as an unlisted AMO add-on) `webext.Browser` configs
- `internal/wm/` - window-manager seam: `wm.Driver` interface (list windows by app bundle ID, move a window to a workspace, focus a window), `wm.FakeDriver`, and the inert `wm.None`
- `internal/aerospace/` - `wm.Driver` over the `aerospace` CLI
- `internal/claude/` - Claude session management
- `internal/tui/` - Bubbletea TUI application; `internal/tui/provider` holds the daemon client and providers (sessions, workspaces, and a browser provider that fetches `GET /browser/sessions` after every sessions load and focuses tabs through `POST /browser/focus`)
- `internal/common/` - shared utilities

### Persistence

All domain data (workspaces, sessions, mux sessions, claude sessions) is stored in SQLite via GORM (`internal/db/`). The database file is created at `<configDir>/utena.db` with WAL mode. Each store takes a `db.Database` interface; tests use `db.OpenInMemory()` for in-memory SQLite. Workspace `config.json` (roots and discovered paths) remains file-based via `afero.Fs`.

### Key Patterns

**Database Module** (internal/db/): `DatabaseModule` wraps the DB with lifecycle hooks. Starts before all other modules (runs migrations), shuts down last. Modules implement `common.ModelProvider` to register their GORM models, and optionally `common.PreMigrationProvider` (`PreMigrations()`) for table/column renames AutoMigrate can't do. Pre-migrations run in one transaction before AutoMigrate and must be idempotent.

**Workspace Manager** (internal/workspace/workspace.go): Uses functional options pattern (`WithRootDir()`) to configure root directories for workspace discovery. Scans directories to find workspace folders.

**Multiplexer** (internal/mux/, internal/tuios/): `mux` owns the domain — `mux.Service` creates/kills multiplexer sessions for utena sessions, tracks their status and windows, and serves attach hooks and activations. It talks to the multiplexer only through the `mux.Driver` interface. `internal/tuios` is the production Driver (only `internal/api/app.go` and `cmd/tui` import it); tests use `mux.FakeDriver`. Supporting another multiplexer means writing another Driver.

**Browser** (internal/browser/, internal/webext/, internal/chrome/, internal/firefox/): `browser` owns the domain — `browser.Service` links sessions to browser windows, opens links in the session's window (deduping by URL prefix and focusing), and serves the `/browser` routes. It talks to browsers only through the `browser.Driver` interface. `internal/webext` is the production Driver, configured by `internal/chrome` or `internal/firefox` (only `internal/api/app.go` imports them; `--browser chrome|firefox`, env `UTENA_BROWSER`, default from `<configDir>/daemon.json` `{"browser": ...}` when the flag isn't given, else `chrome`); tests use `browser.FakeDriver`. One `background.js` serves both browsers: it uses `browser.*` when present, and picks its endpoint and hello `browser` name from its own origin scheme; the driver rejects a hello for another browser. Events are delivered on the `Subscribe` goroutine, never on the driver's read loop, because `Service.Open` holds its lock across driver calls.

Window placement goes through a second seam, `wm.Driver` (internal/wm/). `internal/aerospace` is the production wm driver (only `internal/api` imports it); tests use `wm.FakeDriver`, and `wm.None` (`--wm none`) makes placement inert. `session.SessionService` publishes `session.attached` whenever the attached session changes; `browser.Module` subscribes and calls `Service.SetAttached`, which stores the ID atomically and signals a coalescing placement worker without taking the service lock. The worker links session-owned browser windows to wm windows (cached link, created-window diff, or a unique exact title; never by elimination, and ambiguous windows are left alone) and moves only misplaced ones: the attached session's windows to the browser workspace, every other owned window to the park workspace. Placement never focuses anything and never moves unowned windows. Focus is explicit: when `Open` focuses (attached or activated session, or a session-0 link), it calls `browser.Driver.FocusWindow` and then `wm.FocusWindow` on the window's linked wm window (resolved with the same rules; never guessed), because browsers can't reliably activate themselves and AeroSpace following native focus is not relied on. If no wm window is identified, `Open` logs a warning and still succeeds. `Open` never focuses a window on the park workspace: it places synchronously first when the session it focuses isn't the one placement last saw, and moves a wm window still on the park workspace to the browser workspace before focusing it. The daemon flags `--wm` (`aerospace`|`none`), `--browser-workspace` and `--park-workspace` (env `UTENA_WM`, `UTENA_BROWSER_WORKSPACE`, `UTENA_PARK_WORKSPACE`) live in `internal/api/config.go`.

Window lifecycle: a `BrowserWindow` row is `open`, `missing` or `snapshotted`, and keeps a rolling snapshot of its tabs (`Tabs`, `SnapshotAt`). `TabsChanged` schedules a snapshot at most once per ~2s; only `http(s)` tabs are saved, and an empty tab list never overwrites a saved one; the webext driver coalesces queued `tabs.changed` per window. `WindowRemoved` is held for a 2s grace: if `Disconnected` arrives first, the browser quit and the row becomes `missing` with its tabs (Firefox sends no `WindowRemoved` on quit; its open rows not live at the next `Connected` become `missing` there); if the grace expires, the user closed the window and the row is deleted; `Connected` also flushes pending removals to `missing`. Reattach runs only after a browser restart (a new run ID): unowned live windows are matched to `missing` rows by Jaccard overlap of normalized tab URLs (≥0.5, best first, ties left unmatched, each used once). The restore window (`Service.restoreWindow`) is 0 in production, so it is disabled: reattach happens only against the windows live at `Connected`, and `missing` rows are restored lazily right away. When set (tests only), a restart opens a restore window, which ends early once no `missing` rows remain (its end signals placement), for windows the browser restores late: an empty new window's adoption is deferred until it has URLs; windows rejected at `Connected` and daemon-created session-0 windows are never reattached; a late window with comparable URLs that matches nothing stays unowned. Session archive/delete arrive as `session.archived`/`session.deleted` (`SessionIDEvent`), which `browser.Module` puts on a non-blocking queue drained by a worker; a queued archive is skipped if the session is attached again. Archive clears the attached ID if it is that session, snapshots and closes its live windows (rows already pending removal are skipped, `missing` rows become `snapshotted`, a failed close reopens the row); delete closes its live windows and drops its rows. Both drop deferred adoptions into the session. While disconnected they only update rows, and the windows are closed on the next same-run `Connected`. Restore is lazy: placement restores the attached session when it has no live window, and `Open` restores a session with no live window; restored windows are created unfocused from their saved URLs (a row with no saved tabs is deleted instead), the active tab is reactivated (pinned state isn't), and during the restore window `missing` rows are skipped. A failed row is skipped, not fatal, and `Open` falls back to creating a window when nothing was restored. The extension protocol is `2` (adds `windows.remove`, used by `Driver.CloseWindow`).

Tab lifecycle: each saved tab is `pending`, `open` or `saved` (`TabState`). Only `pending` is stored (`SavedTab.State`); `open` and `saved` come from the window state (`SavedTab.StateIn`), because the bulk row updates never rewrite the tab JSON. Every write of a live window's tabs goes through `mergeTabs`: the live web tabs, plus every pending tab no live tab matches (`MatchTab`). An empty live list keeps the saved tabs. Reattach matching ignores pending tabs. A driver can report `Capabilities` (`CapabilityReporter`): `LoadTakesFocus` makes `Open` for a session that isn't attached (and isn't being activated) hold the link as a pending tab on the session's most recently focused open row, else its most recent restorable row, else a new `snapshotted` row with no driver window, without touching the browser and even while disconnected (`OpenResult.Pending`; a URL the row already has isn't added twice; a matching live tab in an open row is activated, which doesn't take focus, and a row whose window is the focused one gets the tab right away). `FocusChanged` on an owned window opens its pending tabs and clears their marks; a failed one stays pending. Restore loads pending tabs with the saved ones. Placement gives the attached session a window: when the attached session changes or the browser connects with a new run (`ensuredFor`), a session with no live window is restored, or gets a new window with no URLs; a window closed by hand isn't recreated until the next attach. `CreateTakesFocus` makes placement record the focused wm window (`wm.Driver.FocusedWindow`) before restoring or creating; right after each window is created, it waits for focus to move (up to `focusWait`) and focuses the recorded window again after `focusSettle`.

## Dependencies

- **Go**: chi (HTTP router), bubbletea (TUI framework), GORM (ORM) with SQLite driver
- **External**: Requires `tuios` to be installed, CGO_ENABLED=1 for SQLite

## Testing

Tests use in-memory SQLite (`db.OpenInMemory()`) for store and integration tests. Follow Go convention of `*_test.go` files alongside source files. Run tests with `task test`.

## Code Style

### Comments

Do not add comments to code unless explicitly requested by the user. This includes:
- Explanatory comments describing what code does
- Comments documenting functions, methods, or types
- Comments explaining implementation details
- TODOs or FIXMEs (unless specifically asked)

The code should be self-documenting through clear naming and structure. Only add comments when the user explicitly asks for them.
