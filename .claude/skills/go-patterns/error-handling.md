# Error Handling Patterns

Choose between two patterns based on whether the error needs to carry context.

## Sentinel Errors: errors.Is

Use when the error type alone is sufficient -- no additional context needed.

```go
var ErrSessionAlreadyExists = errors.New("session already exists")

if errors.Is(err, ErrSessionAlreadyExists) {
	render.Render(w, r, common.ErrInvalidRequest(err))
	return
}
```

Wrap sentinels with `%w` to add context while preserving the chain:

```go
return fmt.Errorf("session '%s' already exists: %w", session.ID, ErrSessionAlreadyExists)
```

## Custom Error Types: errors.As

Use when callers need information from the error (like which ID was not found).

From `internal/workspace/workspacestore.go`:

```go
type WorkspaceNotFoundError struct {
	WorkspaceID string
}

func (e *WorkspaceNotFoundError) Error() string {
	return "workspace not found: " + e.WorkspaceID
}

return nil, &WorkspaceNotFoundError{WorkspaceID: id}
```

Checking:

```go
var wsNotFound *workspace.WorkspaceNotFoundError
if errors.As(err, &wsNotFound) {
	log.Printf("workspace %s not found", wsNotFound.WorkspaceID)
}
```

## Do NOT Implement Is() on Custom Errors

This creates confusion -- pick one pattern per error:

```go
// Bad: Don't do this
func (e *WorkspaceNotFoundError) Is(target error) bool {
	return target == ErrWorkspaceNotFound
}
```

## Controller Error Handling

Check errors from most specific to least specific. Unknown errors are the fallback.

From `internal/session/sessioncontroller.go`:

```go
func (c *SessionController) CreateSession(w http.ResponseWriter, r *http.Request) {
	// ...
	if err := c.service.CreateSessionAndNotify(ctx, data.Session); err != nil {
		var wsNotFound *workspace.WorkspaceNotFoundError
		if errors.Is(err, ErrSessionAlreadyExists) || errors.As(err, &wsNotFound) {
			render.Render(w, r, common.ErrInvalidRequest(err))
			return
		}
		render.Render(w, r, common.ErrUnknown(err))
		return
	}
	// ...
}
```

## Reusable HTTP Error Responses

From `internal/common/error.go`:

```go
type ErrResponse struct {
	Err            error  `json:"-"`
	HTTPStatusCode int    `json:"-"`
	StatusText     string `json:"status"`
	ErrorText      string `json:"error,omitempty"`
}

func (e *ErrResponse) Render(w http.ResponseWriter, r *http.Request) error {
	render.Status(r, e.HTTPStatusCode)
	return nil
}

func ErrInvalidRequest(err error) render.Renderer { ... }
func ErrNotFound() render.Renderer { ... }
func ErrUnknown(err error) render.Renderer { ... }
```
