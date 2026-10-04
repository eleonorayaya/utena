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

1. **tuios → Daemon**: `TmuxService` holds a long-lived `subscribe` connection on the tuios socket. `session-created`/`session-closed` and window events update `TmuxSession` state and are republished on the eventbus. Every (re)subscribe resyncs from `list-sessions`/`list-windows`.

2. **TUI → Daemon**: The TUI makes HTTP requests to `http://localhost:3333/sessions`. Activating a session publishes `SessionActivated`; `utena attach` long-polls `GET /tmux/activations/next` and restarts its `tuios attach` child on the new session, reporting attach/detach via `PUT /tmux/hooks/{event}`

3. **Daemon API**: Uses chi router, serves on port 3333, mounts controllers:
   - `/sessions` - session management endpoints
   - `/tmux` - client attach hooks, activation long-poll, session env lookup
   - `/claude` - Claude session endpoints
   - `/workspaces` - workspace management endpoints
   - `/todos` - todo management endpoints

### Go Module Structure

- `cmd/daemon/main.go` - daemon entry point
- `cmd/tui/main.go` - TUI entry point
- `internal/api/` - HTTP API and daemon server
- `internal/db/` - SQLite database via GORM (Database interface, migrations)
- `internal/session/` - session controller logic
- `internal/workspace/` - workspace discovery and management
- `internal/tmux/` - tuios service, socket client (`tuiosclient.go`) and controller (package name kept from the tmux era)
- `internal/claude/` - Claude session management
- `internal/todo/` - todo management
- `internal/tui/` - Bubbletea TUI application
- `internal/common/` - shared utilities

### Persistence

All domain data (workspaces, sessions, todos, claude sessions) is stored in SQLite via GORM (`internal/db/`). The database file is created at `<configDir>/utena.db` with WAL mode. Each store takes a `db.Database` interface; tests use `db.OpenInMemory()` for in-memory SQLite. Workspace `config.json` (roots and discovered paths) remains file-based via `afero.Fs`.

### Key Patterns

**Database Module** (internal/db/): `DatabaseModule` wraps the DB with lifecycle hooks. Starts before all other modules (runs migrations), shuts down last. Modules implement `common.ModelProvider` to register their GORM models.

**Workspace Manager** (internal/workspace/workspace.go): Uses functional options pattern (`WithRootDir()`) to configure root directories for workspace discovery. Scans directories to find workspace folders.

**Tmux Service** (internal/tmux/tmuxservice.go): Creates/kills tuios sessions for utena sessions and follows the tuios event stream to track their state. Uses the `tmuxRunner` interface (`tuiosRunner` in production, `MockRunner` in tests).

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
