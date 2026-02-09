# Chi Router Patterns

## Route Ordering (Critical)

Specific routes MUST come before parameterized routes. Chi matches top-to-bottom.

From `internal/session/sessionrouter.go`:

```go
func (sr *SessionRouter) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", sr.controller.ListSessions)
	r.Post("/", sr.controller.CreateSession)
	r.Get("/workspace/{workspaceId}", sr.controller.ListSessionsByWorkspace)
	r.Put("/{name}/activate", sr.controller.ActivateSession)
	r.Get("/{id}", sr.controller.GetSessionByID)
	r.Put("/{id}", sr.controller.UpdateSession)
	r.Delete("/{id}", sr.controller.DeleteSession)

	return r
}
```

`/workspace/{workspaceId}` and `/{name}/activate` come before `/{id}` -- otherwise `/{id}` swallows them.

## Router Struct Pattern

Each domain has its own router struct that returns a `chi.Router`:

```go
type SessionRouter struct {
	controller *SessionController
}

func NewSessionRouter(controller *SessionController) *SessionRouter {
	return &SessionRouter{controller: controller}
}

func (sr *SessionRouter) Routes() chi.Router {
	r := chi.NewRouter()
	// register routes...
	return r
}
```

## Mounting Sub-Routers

From `internal/api/daemon.go`:

```go
func serveAPI(ctx context.Context, ...) {
	r := chi.NewRouter()

	r.Use(httplog.RequestLogger(slog.Default(), &httplog.Options{}))
	r.Use(middleware.Recoverer)
	r.Use(middleware.URLFormat)
	r.Use(render.SetContentType(render.ContentTypeJSON))

	r.Mount("/workspaces", workspaceModule.Routes())
	r.Mount("/sessions", sessionModule.Routes())
	r.Mount("/zellij", zellijModule.Routes())

	http.ListenAndServe(":3333", r)
}
```

## Slog Setup

Must call `slog.SetDefault()` before any goroutines that log:

```go
func StartDaemon() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	// ...
}
```

## Request Binding with render.Bind

Request types implement `Bind(r *http.Request) error` for validation:

```go
type CreateSessionRequest struct {
	*Session
}

func (c *CreateSessionRequest) Bind(r *http.Request) error {
	if c.Session == nil {
		return errors.New("session cannot be nil")
	}
	return ValidateSession(c.Session)
}
```

Controller usage:

```go
func (c *SessionController) CreateSession(w http.ResponseWriter, r *http.Request) {
	data := &CreateSessionRequest{}
	if err := render.Bind(r, data); err != nil {
		render.Render(w, r, common.ErrInvalidRequest(err))
		return
	}
	// ...
}
```

## URL Parameters

```go
id := chi.URLParam(r, "id")
```
