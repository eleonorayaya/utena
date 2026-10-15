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
session (depends on: workspace, git, mux, eventbus)
    ↓
mux (depends on: eventbus; driven by tuios via mux.Driver)
    ↓
monitor (depends on: session, eventbus)
```

`git` sits at the bottom: it never imports another domain, and everything above may call it directly. It notifies upward with `git.EventPRUpdated` on the event bus.

**Key principle**: Dependencies flow downward. Lower modules never depend on higher modules directly. When a lower module needs to notify a higher one, it publishes an event.

### Event Flow

```
tuios event stream → tuios.Driver → mux.Service → EventBus → SessionService
SessionService → (direct calls) → mux.Service → mux.Driver
```

- tuios events (session created/closed) and `utena attach` client hooks (attached, detached, session changed) are published on the event bus and consumed by SessionService to update session state
- SessionService calls `mux.Service` directly for session lifecycle operations (spawn, kill)

`internal/mux` never imports `internal/tuios`; only `internal/api/app.go` and `cmd/tui` do. Supporting another multiplexer means writing another `mux.Driver`. Tests use `mux.FakeDriver`.

See: `internal/eventbus/events.go`, `internal/session/sessionservice.go`, `internal/mux/service.go`, `internal/mux/driver.go`

---

## Communication Patterns

### tuios → Daemon (State Sync)

`mux.Service.followEvents` holds a `Driver.Subscribe`. The tuios driver keeps a `subscribe` connection on the tuios control socket and translates tuios events into `mux.Event`s (`Resync`, `SessionCreated`, `SessionClosed`, `WindowsChanged`).

Flow:
1. On every (re)subscribe the driver emits `Resync`, and `mux.Service` reloads `mux.Session.Status` and window lists via `ListSessions`/`ListWindows`
2. `SessionCreated`/`SessionClosed` update the record and publish on the event bus; a close of a record utena already marked inactive (its own kill) is not republished
3. Window events refresh that session's cached window list
4. SessionService handler updates session state

The daemon never starts tuios itself: a tuios daemon started from launchd would hand every pane, popup and hook launchd's minimal environment. Start tuios from a terminal with `utena attach`; until then, tuios calls fail and the event follower keeps retrying.

See: `internal/mux/service.go`, `internal/tuios/driver.go`

### Daemon → tuios (Session Lifecycle)

SessionService calls `mux.Service` directly to manage multiplexer sessions.

Flow:
1. HTTP POST `/sessions` creates new session record
2. SessionService calls `mux.Service.RegisterPending` to record a pending mux session
3. Background setup goroutine calls `mux.Service` to spawn the session through the driver
4. tuios session becomes active

See: `internal/session/sessionservice.go`, `internal/mux/service.go`

### Resource Usage (`utena top`)

`GET /sessions/resources` reports CPU, memory and process count per multiplexer session.

Flow:
1. `mux.Service.ResourceUsage` lists every process once (`/bin/ps`, behind `mux.ProcessLister`)
2. `Driver.PaneShells` says which pids are pane shells and which session each belongs to. The tuios driver checks the tuios daemon's children with `resolve-pane` and caches the answer per pid
3. Each process is charged to its nearest pane-shell ancestor. CPU is the CPU-time delta since the previous sample (within a minute), else `ps` `%cpu`
4. `SessionService.ResourceUsage` maps each mux session to its utena session, or marks it `unmanaged`
5. `utena top` polls the endpoint (live Bubbletea view, or `--once` for TSV)

Processes that leave the pane's process tree (daemonized and reparented to launchd) are not counted, nor is the tuios daemon itself.

See: `internal/mux/resources.go`, `internal/tuios/driver.go`, `internal/tui/top/`, `cmd/tui/top.go`

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
5. `utena attach` (long-polling `GET /mux/activations/next`) sends SIGTERM to its `tuios attach` child, which detaches, then attaches the new session

See: `internal/tui/provider/client.go`, `cmd/tui/attach.go`
