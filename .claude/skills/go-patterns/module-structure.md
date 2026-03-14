# Module Structure

Each domain is a self-contained package with layered components wired together by a module struct.

## File Layout

```
internal/
  {domain}/
    {domain}.go            # Core types and sentinel errors
    {domain}store.go       # GORM-backed data persistence
    {domain}service.go     # Business logic, cross-store coordination
    {domain}controller.go  # HTTP handlers
    {domain}router.go      # Route definitions
    {domain}module.go      # Wiring constructor and lifecycle
    types.go               # Request/response types with Bind/Render
    validation.go          # Validation functions
```

## Module Struct

The module struct holds all layers and wires them in the constructor:

From `internal/session/sessionmodule.go`:

```go
type SessionModule struct {
	Store      *SessionStore
	Service    *SessionService
	Controller *SessionController
	Router     *SessionRouter
}

func NewSessionModule(tmuxService *utmux.TmuxService, workspaceModule *workspace.WorkspaceModule, bus eventbus.EventBus, database db.Database, branchPrefix string) *SessionModule {
	store := NewSessionStore(database)
	service := NewSessionService(store, workspaceModule.Service, workspaceModule.GitService, tmuxService, bus, branchPrefix)
	controller := NewSessionController(service)
	router := NewSessionRouter(controller)

	return &SessionModule{
		Store:      store,
		Service:    service,
		Controller: controller,
		Router:     router,
	}
}
```

## Lifecycle Interface

From `internal/common/lifecycle.go`:

```go
type Module interface {
	OnAppStart(ctx context.Context) error
	OnAppEnd(ctx context.Context) error
	Routes() chi.Router
}

type ModelProvider interface {
	Models() []any
}
```

Module implements lifecycle by delegating to components in order:

```go
func (m *SessionModule) OnAppStart(ctx context.Context) error {
	if err := m.Store.OnAppStart(ctx); err != nil {
		return err
	}
	if err := m.Service.OnAppStart(ctx); err != nil {
		return err
	}
	return nil
}

func (m *SessionModule) OnAppEnd(ctx context.Context) error {
	if err := m.Service.OnAppEnd(ctx); err != nil {
		return err
	}
	if err := m.Store.OnAppEnd(ctx); err != nil {
		return err
	}
	return nil
}
```

OnAppStart: store first (no-op for GORM stores since migration is handled by DatabaseModule), then service (subscribes to events).
OnAppEnd: reverse order -- service first, then store.

Modules that own database models implement `ModelProvider`:

```go
func (m *SessionModule) Models() []any {
	return []any{&Session{}}
}
```

The app collects models from all modules and registers them with the database before starting modules.

## Daemon Wiring

The app (`internal/api/app.go`) creates a `DatabaseModule` first, collects models from all domain modules, then starts everything in dependency order. The database starts before all other modules and shuts down last.

Module startup order: database → workspace → session → tmux → others.
Module shutdown order: reverse.

## Dependency Flow

- Store: depends on `db.Database` interface only
- Service: depends on own store + other module services + event bus
- Controller: depends on own service only
- Router: depends on own controller only
- Module: accepts `db.Database` and other external dependencies, creates internal layers
