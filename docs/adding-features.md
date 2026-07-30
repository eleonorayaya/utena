# Adding Features

> **Prerequisites**: Read `docs/architecture.md` for system orientation and `docs/backend-patterns.md` for coding conventions before using this guide.

## Adding a New HTTP Endpoint

### 1. Add Service Method

**Required**: Business logic goes in service layer.

```go
// internal/session/sessionservice.go
func (s *SessionService) DoSomething(ctx context.Context, id string) error {
    // Business logic here

    // If other modules need to know, publish event
    event := eventbus.Event{Type: "session.something_happened", Data: ...}
    s.eventBus.Publish(ctx, event)

    return nil
}
```

### 2. Add Controller Method

**Required**: Keep controllers thin - just HTTP concerns.

```go
// internal/session/sessioncontroller.go
func (c *SessionController) DoSomething(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    id := chi.URLParam(r, "id")

    if err := c.service.DoSomething(ctx, id); err != nil {
        render.Render(w, r, common.ErrUnknown(err))
        return
    }

    render.NoContent(w, r)
}
```

### 3. Add Route

```go
// internal/session/sessionrouter.go
r.Post("/{id}/action", sr.controller.DoSomething)
```

## Adding Event-Based Communication

See `docs/backend-patterns.md` Pattern 7 for the full event bus pattern including when to use it, how to define events, publish, and subscribe.

## Adding a Session Event for Claude's Monitor

Each Claude session runs `utena monitor $UTENA_SESSION_ID` as a plugin monitor (`plugins/utena-claude/monitors/monitors.json`), which holds a websocket to `GET /monitor/ws?session_id=<id>`. Every text frame becomes one notification in that Claude session.

The monitor module is transport only — it holds no domain state and depends on no other domain. To feed a new kind of event to Claude, publish from the service that owns the data:

```go
s.eventBus.Publish(ctx, eventbus.Event{
    Type: eventbus.SessionNotification,
    Data: eventbus.SessionNotificationEvent{
        SessionID: sess.ID,
        Type:      "build_failed",
        Data:      buildFailedPayload{...},
    },
})
```

`MonitorService` marshals it to `{"type":..., "session_id":..., "data":...}` and fans it out to the websockets watching that session. Only publish on an actual change — every event is a message in Claude's context.

On connect, the monitor asks `SnapshotProvider` (implemented by `SessionService.SessionSnapshot`) for the session's current state so a session that starts after a change is not left with silence. Add the same event type there when the current state matters at connect time.

### Current event types

| Type | Fires when |
|---|---|
| `pull_request` | PR state, title or assignment changes (`git.prs` job, 30s). Also sent on connect for the session's open PRs |
| `pull_request_review` | Someone submits a review — `state` is `approved` / `changes_requested` / `commented` |
| `pull_request_review_comment` | Someone leaves an inline comment, with `path` and `line` |
| `ci_checks` | `failing` the first time a check fails, then `passed` / `failed` once every run completes, with the failed check names |

Reviews and comments authored by the daemon's own GitHub user, or by bots, are dropped.

### Polling activity without burning the API budget

`SessionService.SyncPRActivity` (job `session.pr_activity`, 60s) is the driver, not the git module: only PRs whose head branch belongs to a live session are worth polling, and session is the module that knows which those are. Each PR costs up to three GitHub calls per tick, so keep that scoping if you add a source.

`GitService.SyncPRActivity` owns the GitHub calls and the change detection. Watermarks live on the `PullRequest` row (`ActivityBaselined`, `LastReviewID`, `LastReviewCommentID`, `ChecksHeadSHA`, `ChecksState`), so a daemon restart does not replay old activity. Two rules worth preserving:

- The first sync of a PR records a baseline and emits nothing — otherwise adopting a PR would dump its history into Claude's context, and a fleet of already-green PRs would each fire a rollup at once. `ActivityBaselined` is a separate flag rather than "watermark == 0", because a PR with no reviews yet also has 0 and would lose its *first* review.
- `syncGitHubPR` rebuilds a PR from the GitHub payload and writes every other column, so it passes `activityColumns()` to `PRStore.Update` to leave these alone. A new activity column goes in that list.

The wire shapes for all four PR event types live together in `internal/git/practivity.go` (`NotificationPullRequest` and friends, `PRNotification`, `ReviewActivity`, `ReviewCommentActivity`, `ChecksActivity`). Session decides *who* gets an event; git decides what one looks like.

See: `internal/monitor/`, `internal/git/practivity.go`, `internal/session/sessionservice.go` (`notifyPRUpdated`, `SyncPRActivity`, `SessionSnapshot`)

## Adding a New Module

### 1. Create Module Structure

Create files following the pattern in `internal/session/`:
- `types.go` - Data structures
- `store.go` - Data persistence
- `service.go` - Business logic
- `controller.go` - HTTP handlers
- `router.go` - Route definitions
- `module.go` - Composition

### 2. Implement Lifecycle

**Required**: Implement these in module.go:

```go
func (m *Module) OnAppStart(ctx context.Context) error
func (m *Module) OnAppEnd(ctx context.Context) error
func (m *Module) Routes() chi.Router
```

### 3. Wire in App

Add the module to `buildApp()` in `internal/api/app.go`:

```go
newModule := newmodule.NewModule(dependencies, bus)
```

Then add it to the `modules()` slice in dependency order. The app calls `OnAppStart` and `OnAppEnd` on all modules automatically — modules are shut down in reverse order.

Mount its routes in `Routes()`:

```go
r.Mount("/path", app.NewModule.Routes())
```

See: `internal/api/app.go`

## Testing New Features

### Service Tests

Test business logic with mocked dependencies.

See: `internal/session/sessionservice_test.go`

### Controller Tests

Use httptest to test HTTP handling.

See: `internal/session/sessionrouter_test.go`

### Integration Tests

Test full module stack with real HTTP requests.

See: `internal/api/daemon_test.go`
