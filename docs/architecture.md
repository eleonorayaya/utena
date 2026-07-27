# Architecture Overview

> **Reading order**: Start here for orientation. Then read `docs/backend-patterns.md` for coding conventions. When building something specific, see `docs/adding-features.md`.

---

## System Components

Utena consists of three main components:
- **daemon** — HTTP API server managing workspace and session state
- **tui** — Terminal UI client for user interaction
- **tmux plugin** — TPM plugin that registers tmux hooks to sync state with the daemon

---

## Daemon Architecture

### Module Dependencies

```
workspace (no dependencies)
    ↓
session (depends on: workspace, eventbus)
    ↓
tmux (depends on: session, eventbus)
```

**Key principle**: Dependencies flow downward. Lower modules never depend on higher modules directly. When a lower module needs to notify a higher one, it publishes an event.

### Event Flow

```
tmux hooks → TmuxService → EventBus → SessionService
SessionService → (direct calls) → TmuxService
```

- Tmux hook events (session created, client attached, etc.) are published on the event bus and consumed by SessionService to update session state
- SessionService calls TmuxService directly for session lifecycle operations (spawn, kill)

See: `internal/eventbus/events.go`, `internal/session/sessionservice.go`, `internal/tmux/tmuxservice.go`

---

## Communication Patterns

### tmux hooks → Daemon (State Sync)

HTTP PUT `/tmux/hooks/{event}` with session name from tmux hook.

Flow:
1. tmux fires a hook (e.g., session-created, client-session-changed)
2. TPM plugin's `hook.sh` sends HTTP request to daemon
3. TmuxController receives request, extracts event type
4. TmuxService publishes event on event bus
5. SessionService handler updates session state

See: `internal/tmux/tmuxservice.go`

### Daemon → tmux (Session Lifecycle)

SessionService calls TmuxService directly to manage tmux sessions.

Flow:
1. HTTP POST `/sessions` creates new session record
2. SessionService calls TmuxService to register a pending tmux session
3. Background setup goroutine calls TmuxService to spawn the session
4. Tmux session becomes active

See: `internal/session/sessionservice.go`, `internal/tmux/tmuxservice.go`

### Daemon → Claude (Session Events)

At session start, the `utena-claude` plugin's monitor runs `utena monitor $UTENA_SESSION_ID`, which opens a websocket to `GET /monitor/ws?session_id=<id>` and echoes each frame to stdout. The daemon pushes one JSON text frame per event; Claude receives each stdout line as a notification.

Plugin monitors only accept a shell `command`, not the Monitor tool's `ws` input — hence the thin client. It retries every 5s, so it survives a daemon restart.

Flow:
1. A service notices a change it wants Claude to know about (e.g. `SessionService.handlePRUpdated` sees a PR state change)
2. It publishes `eventbus.SessionNotification` with the session ID
3. `MonitorService` marshals the event and fans it out to that session's sockets
4. On connect, `MonitorService` first sends a snapshot of current state from `SessionService.SessionSnapshot`

See: `internal/monitor/`, `cmd/tui/monitor.go`, `plugins/utena-claude/monitors/monitors.json`, `docs/adding-features.md`

### TUI → Daemon

HTTP requests to fetch session/workspace data. Session switching via `tmux switch-client`.

Flow:
1. TUI fetches `/sessions` endpoint
2. SessionController returns current state
3. TUI renders in terminal
4. User selects session → TUI calls `tmux switch-client -t <name>`

See: `internal/tui/provider/client.go`
