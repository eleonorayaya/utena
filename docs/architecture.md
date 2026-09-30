# Architecture Overview

> **Reading order**: Start here for orientation. Then read `docs/backend-patterns.md` for coding conventions. When building something specific, see `docs/adding-features.md`.

---

## System Components

Utena consists of two main components, driving [tuios](https://tuios.dev):
- **daemon** — HTTP API server managing workspace and session state
- **tui** — Terminal UI client for user interaction

---

## Daemon Architecture

### Module Dependencies

```
git (no dependencies)
workspace (depends on: git)
    ↓
session (depends on: workspace, git, tmux, eventbus)
    ↓
tmux (depends on: eventbus)
    ↓
monitor (depends on: session, eventbus)
```

`git` sits at the bottom: it never imports another domain, and everything above may call it directly. It notifies upward with `git.EventPRUpdated` on the event bus.

**Key principle**: Dependencies flow downward. Lower modules never depend on higher modules directly. When a lower module needs to notify a higher one, it publishes an event.

### Event Flow

```
tuios event stream → TmuxService → EventBus → SessionService
SessionService → (direct calls) → TmuxService
```

- tuios events (session created/closed) and `utena attach` client hooks (attached, detached, session changed) are published on the event bus and consumed by SessionService to update session state
- SessionService calls TmuxService directly for session lifecycle operations (spawn, kill)

See: `internal/eventbus/events.go`, `internal/session/sessionservice.go`, `internal/tmux/tmuxservice.go`

---

## Communication Patterns

### tuios → Daemon (State Sync)

`TmuxService.followEvents` holds a `subscribe` connection on the tuios control socket.

Flow:
1. On every (re)subscribe, TmuxService resyncs `TmuxSession.Status` and window lists from `list-sessions`/`list-windows`
2. `session-created`/`session-closed` update the record and publish on the event bus; a close of a record utena already marked inactive (its own kill) is not republished
3. Window events refresh that session's cached window list
4. SessionService handler updates session state

If the tuios daemon is down, the runner runs `tuios start-server`, which restores saved sessions.

See: `internal/tmux/tmuxservice.go`, `internal/tmux/tuiosclient.go`

### Daemon → tuios (Session Lifecycle)

SessionService calls TmuxService directly to manage tuios sessions.

Flow:
1. HTTP POST `/sessions` creates new session record
2. SessionService calls TmuxService to register a pending tmux session
3. Background setup goroutine calls TmuxService to spawn the session
4. tuios session becomes active

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

HTTP requests to fetch session/workspace data. tuios has no programmatic `switch-client`, so switching goes through the `utena attach` wrapper.

Flow:
1. TUI fetches `/sessions` endpoint
2. SessionController returns current state
3. TUI renders in terminal
4. User selects session → TUI calls `PUT /sessions/{id}/activate` → `SessionActivated` is published
5. `utena attach` (long-polling `GET /tmux/activations/next`) sends SIGTERM to its `tuios attach` child, which detaches, then attaches the new session

See: `internal/tui/provider/client.go`, `cmd/tui/attach.go`
