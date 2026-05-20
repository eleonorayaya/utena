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
