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

2. **TUI → Daemon**: The TUI makes HTTP requests to `http://localhost:3333/sessions`. Activating a session publishes `SessionActivated`; `utena attach` long-polls `GET /mux/activations/next` and restarts its `tuios attach` child on the new session, reporting attach/detach via `PUT /mux/hooks/{event}`

3. **Daemon API**: Uses chi router, serves on port 3333, mounts controllers:
   - `/sessions` - session management endpoints
   - `/mux` - client attach hooks, activation long-poll, session env lookup
   - `/claude` - Claude session endpoints
   - `/workspaces` - workspace management endpoints

### Go Module Structure

- `cmd/daemon/main.go` - daemon entry point
- `cmd/tui/main.go` - TUI entry point
- `internal/api/` - HTTP API and daemon server
- `internal/db/` - SQLite database via GORM (Database interface, migrations)
- `internal/session/` - session controller logic
- `internal/workspace/` - workspace discovery and management
- `internal/mux/` - multiplexer-agnostic session domain: `mux.Session` model, store, service, `/mux` controller, `Driver` interface
- `internal/tuios/` - `mux.Driver` over the tuios control socket, agent-pane tagging, tuios workspace CLI helpers
- `internal/claude/` - Claude session management
- `internal/tui/` - Bubbletea TUI application
- `internal/common/` - shared utilities

### Persistence

All domain data (workspaces, sessions, mux sessions, claude sessions) is stored in SQLite via GORM (`internal/db/`). The database file is created at `<configDir>/utena.db` with WAL mode. Each store takes a `db.Database` interface; tests use `db.OpenInMemory()` for in-memory SQLite. Workspace `config.json` (roots and discovered paths) remains file-based via `afero.Fs`.

### Key Patterns

**Database Module** (internal/db/): `DatabaseModule` wraps the DB with lifecycle hooks. Starts before all other modules (runs migrations), shuts down last. Modules implement `common.ModelProvider` to register their GORM models, and optionally `common.PreMigrationProvider` (`PreMigrations()`) for table/column renames AutoMigrate can't do. Pre-migrations run in one transaction before AutoMigrate and must be idempotent.

**Workspace Manager** (internal/workspace/workspace.go): Uses functional options pattern (`WithRootDir()`) to configure root directories for workspace discovery. Scans directories to find workspace folders.

**Multiplexer** (internal/mux/, internal/tuios/): `mux` owns the domain — `mux.Service` creates/kills multiplexer sessions for utena sessions, tracks their status and windows, and serves attach hooks and activations. It talks to the multiplexer only through the `mux.Driver` interface. `internal/tuios` is the production Driver (only `internal/api/app.go` and `cmd/tui` import it); tests use `mux.FakeDriver`. Supporting another multiplexer means writing another Driver.

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
