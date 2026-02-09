# Module Structure

Each domain is a self-contained package with layered components wired together by a module struct.

## File Layout

```
internal/
  {domain}/
    {domain}.go            # Core types and sentinel errors
    {domain}store.go       # In-memory data persistence
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

func NewSessionModule(workspaceModule *workspace.WorkspaceModule, bus eventbus.EventBus, fs afero.Fs, configDir string) *SessionModule {
	store := NewSessionStore(fs, configDir)
	service := NewSessionService(store, workspaceModule.Store, bus)
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

OnAppStart: store first (loads data), then service (subscribes to events).
OnAppEnd: reverse order -- service first, then store.

## Daemon Wiring

From `internal/api/daemon.go`:

```go
func StartDaemon() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bus := eventbus.NewEventBus()

	workspaceModule := workspace.NewWorkspaceModule()
	sessionModule := session.NewSessionModule(workspaceModule, bus, afero.NewOsFs(), configDir)
	zellijModule := zellij.NewZellijModule(sessionModule, bus)

	workspaceModule.OnAppStart(ctx)
	sessionModule.OnAppStart(ctx)
	zellijModule.OnAppStart(ctx)

	go serveAPI(ctx, workspaceModule, sessionModule, zellijModule)

	<-ctx.Done()

	zellijModule.OnAppEnd(ctx)
	sessionModule.OnAppEnd(ctx)
	workspaceModule.OnAppEnd(ctx)
}
```

Module startup order: dependencies first (workspace before session before zellij).
Module shutdown order: reverse (zellij before session before workspace).

## Dependency Flow

- Store: no dependencies (or just afero.Fs)
- Service: depends on own store + other module stores + event bus
- Controller: depends on own service only
- Router: depends on own controller only
- Module: accepts external dependencies, creates internal layers
