# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Utena is a workspace management system for tuios, consisting of two interconnected components:
- **daemon**: HTTP API server (Go) that manages workspace state and session information
- **tui**: Terminal UI client (Go + Bubbletea) for interacting with the daemon

The daemon drives tuios over its JSON control socket and follows the tuios event stream to track session and window state. The TUI queries the daemon to display workspace information and is launched from a tuios popup keybinding, or by `utena picker` in its own terminal window.

## Build & Run Commands

This project uses [Task](https://taskfile.dev) as its build system. NEVER run `go`, `cargo`, or other build commands directly. NEVER `cd` into subdirectories to run commands. ALWAYS use `task <target>` from the project root; `task --list-all` shows the targets.

Key commands: `task daemon:run`, `task tui:run`, `task fmt`, `task test`. Builds need CGO_ENABLED=1 (SQLite) and `tuios` installed.

## Architecture

### Component Communication

1. **tuios → Daemon**: `mux.Service` holds a long-lived `Driver.Subscribe`; the tuios driver translates the tuios event stream into `mux.Event`s. Session created/closed and window events update `mux.Session` state and are republished on the eventbus (`mux.*` events). Every (re)subscribe emits `Resync`, which reloads from `ListSessions`/`ListWindows`.

2. **TUI → Daemon**: The TUI makes HTTP requests to `http://127.0.0.1:3333/sessions`. Activating a session publishes `SessionActivated`; `utena attach` long-polls `GET /mux/activations/next` and restarts its `tuios attach` child on the new session, reporting attach/detach via `PUT /mux/hooks/{event}`

3. **Daemon API**: Uses chi router, serves on port 3333 and listens on 127.0.0.1 only (clients must use `127.0.0.1`, not `localhost`), mounts controllers:
   - `/sessions` - session management endpoints (`GET /sessions/resources` backs `utena top`; `?archived=true` adds archived sessions with disk use only, `PUT /sessions/{id}/bundles` sets a session's plugin bundles, `GET /sessions/default-bundles?workspace_id=…` unions workspaces' `default_bundles` for the new-session form)
   - `/bundles` - plugin bundle catalog from daemon.json
   - `/mux` - client attach hooks, activation long-poll, session env lookup
   - `/claude` - Claude session endpoints
   - `/workspaces` - workspace management endpoints
   - `/browser` - open links in session windows (`POST /open`), driver status, per-session window and tab state for the TUI (`GET /sessions`), tab focus that activates the session first if needed (`POST /focus`), releasing a window from its session (`POST /eject`), moving a tab to another session's window (`POST /move`), and the extension websocket at `/browser/<driver name>` (`/browser/chrome` or `/browser/firefox`)

### Go Module Structure

- `internal/mux/` - multiplexer-agnostic session domain: `mux.Session` model, store, service, `/mux` controller, `Driver` interface
- `internal/tuios/` - `mux.Driver` over the tuios control socket, agent-pane tagging, tuios workspace CLI helpers
- `internal/leader/` - a follower's address for its leader (`Address`, shared by the leader source reader and the follower browser module) and `PUT /leader`, which the leader calls after each connect; 404 on a leader
- `internal/browser/` - browser-agnostic domain: session→window ownership, open/dedup/focus rules, `/browser` controller, `Driver` interface
- `internal/webext/` - shared `browser.Driver` over a websocket to the utena WebExtension (`extension/background.js`), parametrized by a `webext.Browser` (name, app bundle ID, extension origin scheme, wm window-title suffix, outdated hint)
- `internal/chromescript/` - `browser.Driver` for Chrome over `osascript` (no extension): polls windows and tabs every ~1.5s and diffs them into events, reports `CreateTakesFocus`/`LoadTakesFocus`, and a `WarningReporter` hint while Automation is denied (`-1743`)
- `internal/chrome/`, `internal/firefox/` - the Chrome (`extension/chrome`, MV3) and Firefox (`extension/firefox`, MV2 persistent background, signed as an unlisted AMO add-on) `webext.Browser` configs
- `internal/wm/` - window-manager seam: `wm.Driver` interface (list windows by app bundle ID, move a window to a workspace, focus a window), `wm.FakeDriver`, and the inert `wm.None`
- `internal/picker/` - `utena picker`: focuses the open picker window (exact title among the `--app-id` terminal's wm windows), or launches the terminal command given after `--` with `/usr/bin/env UTENA_PICKER=1 UTENA_PICKER_RETURN_WINDOW=<focused wm window> <utena>` appended. In picker mode the TUI also quits on esc in the session list, and on exit `ReturnFocus` focuses the recorded window (no-op without it or when AeroSpace is unavailable). The `Launcher` seam starts the terminal detached; tests use fakes
- `internal/claudedata/` - Claude Code's on-disk session data (`claudesettings.ClaudeDataDir()`: `$CLAUDE_CONFIG_DIR` or `~/.claude`): project dirs under `projects/`, owned by the first `cwd` in their transcripts (dir names are lossy, so never matched by name), plus `file-history/<id>` and `session-env/<id>`. Deleting a session removes the project dirs at or under its root (candidates are narrowed by the encoded dir name, then confirmed by `cwd`) unless another non-deleted session uses that root. Archived sessions keep their data so they can be resumed, and Claude data utena didn't create is never touched
- `internal/coder/` - leader side of the `coder` session backend: `daemon.json` `"coder"` block (`template`, `parameters`, `ssh_suffix`, `follower_port`, `idle_disconnect`; `daemon.local.json` is read over `daemon.json`, see `loadDaemonFile` in `internal/api/config.go`), the workspace slug, the Coder workspace lifecycle through the `coder` CLI, the per-workspace `ssh -L/-R` forward plus `tuios hosts` entry (`ConnManager`; each connect attempt picks a random remote port for `-R` and sends it to the follower's `PUT /leader`), the follower API client, status polling, and `GET /coder/status`, `PUT /coder/sessions/{id}/refresh`. Every external command goes through the `Runner` seam; tests use fakes
- `internal/bundles/` - plugin bundle catalog (`daemon.json` `"bundles"`, with plugins and MCP server names, plus `"mcp_servers"` definitions) and `GET /bundles`; sessions get plugins in `.claude/settings.local.json` and servers in `.mcp.json`
- `internal/tui/` - Bubbletea TUI application; `internal/tui/bundlepicker` is the plugin bundle multi-select used by the new-session form and the session list's focused detail panel (`p`); `internal/tui/sessionpicker` is the single-select session list the detail panel opens to move a tab (`m`); `internal/tui/sessiondetail` is that panel, a child model of `internal/tui/sessionlist` that `tab` focuses; `internal/tui/provider` holds the daemon client and providers (sessions, workspaces, and a browser provider that fetches `GET /browser/sessions` after every sessions load and focuses tabs through `POST /browser/focus`)
- `internal/diskusage/` - directory size cache for `utena top`: sizes are walked one root at a time in the background and refreshed after `max(TTL, 10× walk time)`, so `GET /sessions/resources` never blocks on disk

### Persistence

All domain data (workspaces, sessions, mux sessions, claude sessions) is stored in SQLite via GORM (`internal/db/`). The database file is created at `<configDir>/utena.db` with WAL mode. Each store takes a `db.Database` interface; tests use `db.OpenInMemory()` for in-memory SQLite. Workspace `config.json` (roots and discovered paths) remains file-based via `afero.Fs`.

Subsystem rules (coder backend, session setup, database module, workspace manager, multiplexer, browser) live in the `CLAUDE.md` of each package: `internal/coder/`, `internal/session/`, `internal/db/`, `internal/workspace/`, `internal/mux/`, `internal/browser/`. Read the relevant one before changing that subsystem.

## Go Conventions

- Each domain package is layered store/service/controller/router/module, wired by the module constructor; modules implement `common.ModelProvider` to register GORM models. Copy the shape of an existing domain (e.g. `internal/workspace/`) for a new one.
- Accept interfaces, return structs; inject `db.Database` and other dependencies through constructors.
- Stores query GORM/SQLite directly: no in-memory maps, mutexes or defensive copies. Map `gorm.ErrRecordNotFound` to the domain's sentinel error; load associations with `Joins("Workspace")` and skip them on writes with `Omit("Workspace")`.
- Use sentinel errors (`errors.Is`) by default, a custom error type (`errors.As`) when the error carries context such as an ID.
- Use `afero.Fs` only for non-database file I/O (e.g. workspace `config.json`).
- When one module needs to trigger behavior in another without depending on it, publish an event on `internal/eventbus`.

## Testing

Store and integration tests use in-memory SQLite (`db.OpenInMemory()`). Run tests with `task test`.

## Code Style

Do not add comments to code (explanations, doc comments, TODOs or FIXMEs) unless the user explicitly asks for them.
