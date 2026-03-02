# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Utena is a workspace management system for tmux, consisting of three interconnected components:
- **daemon**: HTTP API server (Go) that manages workspace state and session information
- **tui**: Terminal UI client (Go + Bubbletea) for interacting with the daemon
- **tmux plugin**: TPM plugin (bash) that registers tmux hooks to sync session state with the daemon

The architecture enables tmux hooks to detect session changes and push updates to the daemon via HTTP, while the TUI can query the daemon to display workspace information. The TUI is launched via `tmux display-popup`.

## Build & Run Commands

This project uses [Task](https://taskfile.dev) as its build system. NEVER run `go`, `cargo`, or other build commands directly. NEVER `cd` into subdirectories to run commands. ALWAYS use `task <target>` from the project root.

See the `taskfile-commands` skill for the full command reference.

Key commands: `task daemon:run`, `task tui:run`, `task fmt`, `task test`.

## Architecture

### Component Communication

1. **tmux hooks → Daemon**: The TPM plugin registers tmux hooks (session-created, session-closed, client-session-changed, client-attached, client-detached) that send HTTP PUT requests to `http://localhost:3333/tmux/hooks/{event}` with session name

2. **TUI → Daemon**: The TUI makes HTTP requests to `http://localhost:3333/sessions` to fetch workspace/session data and uses `tmux switch-client` to switch sessions

3. **Daemon API**: Uses chi router, serves on port 3333, mounts controllers:
   - `/sessions` - session management endpoints
   - `/tmux` - tmux hook endpoints
   - `/claude` - Claude session endpoints
   - `/workspaces` - workspace management endpoints
   - `/todos` - todo management endpoints

### Go Module Structure

- `cmd/daemon/main.go` - daemon entry point
- `cmd/tui/main.go` - TUI entry point
- `internal/api/` - HTTP API and daemon server
- `internal/session/` - session controller logic
- `internal/workspace/` - workspace discovery and management
- `internal/tmux/` - tmux service and controller
- `internal/claude/` - Claude session management
- `internal/todo/` - todo management
- `internal/tui/` - Bubbletea TUI application
- `internal/common/` - shared utilities
- `plugins/utena-tmux/` - TPM plugin scripts

### Key Patterns

**Workspace Manager** (internal/workspace/workspace.go): Uses functional options pattern (`WithRootDir()`) to configure root directories for workspace discovery. Scans directories to find workspace folders.

**Tmux Service** (internal/tmux/tmuxservice.go): Handles tmux hook events to track session state. Subscribes to eventbus events to create/kill tmux sessions when utena sessions are created/deleted.

**TPM Plugin** (plugins/utena-tmux/utena.tmux): Registers tmux hooks that curl the daemon's hook endpoint. Binds `prefix + p` to open the TUI in a display-popup.

## Dependencies

- **Go**: chi (HTTP router), bubbletea (TUI framework)
- **External**: Requires tmux terminal multiplexer to be installed

## Testing

Currently no test infrastructure is set up. When adding tests, follow Go convention of `*_test.go` files alongside source files.

## Code Style

### Comments

Do not add comments to code unless explicitly requested by the user. This includes:
- Explanatory comments describing what code does
- Comments documenting functions, methods, or types
- Comments explaining implementation details
- TODOs or FIXMEs (unless specifically asked)

The code should be self-documenting through clear naming and structure. Only add comments when the user explicitly asks for them.
