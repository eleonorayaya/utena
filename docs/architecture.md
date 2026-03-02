# Architecture Overview

## System Components

Utena consists of three main components:
- **daemon** - HTTP API server managing workspace and session state
- **tui** - Terminal UI client for user interaction
- **tmux plugin** - TPM plugin that registers tmux hooks to sync state with the daemon

## Daemon Architecture

### Module Dependencies

```
workspace (no dependencies)
    ↓
session (depends on: workspace, eventbus)
    ↓
tmux (depends on: session, eventbus)
```

**Key principle**: Dependencies flow downward. Lower modules never depend on higher modules directly.

### Event Flow

```
session → (events) → eventbus → (events) → tmux
tmux → (direct calls) → session
```

**Rationale**:
- Session doesn't know about tmux (no dependency)
- Tmux needs to update session state (direct calls)
- Session needs to notify tmux of user actions (events)

See: `docs/event-bus.md` for details

## Communication Patterns

### tmux hooks → Daemon (State Sync)

HTTP PUT `/tmux/hooks/{event}` with session name from tmux hook.

Flow:
1. tmux fires a hook (e.g., session-created, client-session-changed)
2. TPM plugin's hook.sh sends HTTP request to daemon
3. TmuxController receives request, extracts event type
4. TmuxService calls SessionService methods directly
5. Session state updated

See: `internal/tmux/tmuxservice.go`

### Daemon → tmux (Session Lifecycle)

Event bus triggers tmux session creation/deletion.

Flow:
1. HTTP POST `/sessions` creates new session
2. SessionService publishes `SessionCreateRequested` event
3. TmuxService subscribed to event
4. TmuxService creates tmux session via `tmux new-session`

See:
- Service publishing: `internal/session/sessionservice.go`
- Tmux subscribing: `internal/tmux/tmuxservice.go`

### TUI → Daemon

HTTP requests to fetch session/workspace data. Session switching via `tmux switch-client`.

Flow:
1. TUI fetches `/sessions` endpoint
2. SessionController returns current state
3. TUI renders in terminal
4. User selects session → TUI calls `tmux switch-client -t <name>`

See: `internal/tui/provider/client.go`

## Dependency Inversion

When Module A needs Module B's functionality but direct dependency would create a cycle:

1. **Preferred**: Use event bus for one direction
2. **Alternative**: Extract shared interface to common package

Example: Session and tmux modules would create a cycle if Session depended on tmux. Instead, Session publishes events that tmux subscribes to.

## Testing

Each layer can be tested independently:

- **Stores**: Test with in-memory data
- **Services**: Mock store dependencies
- **Controllers**: Use httptest with real service
- **Integration**: Test full module stack

See test files adjacent to source files (e.g., `internal/session/sessionservice_test.go`)
