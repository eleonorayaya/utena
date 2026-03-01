# Move Navigation to Shared `router` Package, Make Router Generic

## Context

The router currently imports every view subpackage for two reasons: (1) to construct concrete models in `NewRouter`, and (2) to handle per-model navigation messages in the `Update` switch. Both can be eliminated. Navigation messages move to a shared `router` package (`NavigateToMsg`, `BackMsg`), and view construction moves to the app. The router becomes fully generic — it operates only on a `viewEntry` interface and `router.View` enum.

## Plan

### 1. Create `internal/tui/router/router.go`

The view enum (currently `view` in router.go) and two navigation message types + cmd constructors:

```go
package router

type View int

const (
    SessionListView View = iota
    SessionFormView
    TodoListView
    TodoFormView
    DebugView
)

type NavigateToMsg struct{ Target View }
type BackMsg struct{}

func NavigateTo(target View) tea.Cmd { ... }
func Back() tea.Cmd { ... }
```

### 2. Keep existing interface names

- `viewEntry` stays as-is in `internal/tui/router.go` — the non-generic type-erased interface for the router map
- `ViewModel[T]` stays as-is in `internal/tui/model.go` — the generic interface for compile-time checks
- `viewAdapter[T]` stays as-is — bridges concrete models to `viewEntry`

### 3. Make `NewRouter` accept the view map

```go
func NewRouter(views map[router.View]viewEntry, initialView router.View) Router
```

The app constructs the concrete view map and passes it in. The router no longer imports any view subpackage.

### 4. Use a view stack for back navigation

Replace the single `previousView` field with a stack. `NavigateToMsg` pushes, `BackMsg` pops. This correctly handles multi-level back (sessionList → todoList → todoForm → back → back).

### 5. Simplify router's `Update` switch

```go
case router.NavigateToMsg:
    return r.navigateTo(msg.Target)
case router.BackMsg:
    return r.navigateBack()
```

The `?` debug key still lives in the router's `OnKeyMsg` — it references `router.DebugView` from the shared enum (no view subpackage import needed).

### 6. Update view subpackages

Replace per-model navigation messages with `router.NavigateTo()`/`router.Back()`:

| Package | Old | New |
|---------|-----|-----|
| sessionlist | `NewSessionMsg{}` | `router.NavigateTo(router.SessionFormView)` |
| sessionlist | `TodosMsg{}` | `router.NavigateTo(router.TodoListView)` |
| sessionform | `BackMsg{}` | `router.Back()` |
| todolist | `NewTodoMsg{}` | `router.NavigateTo(router.TodoFormView)` |
| todolist | `BackMsg{}` | `router.Back()` |
| todoform | `BackMsg{}` | `router.Back()` |
| todoform | `DoneMsg{}` | `router.Back()` |
| debug | `BackMsg{}` | `router.Back()` |

Delete empty `messages.go` files. Keep `messages.go` files that still have non-navigation messages (e.g., picker messages like `workspacepicker.SelectedMsg`).

### 7. Move view construction to `app.go`

`NewApp` builds the view map and passes it to `NewRouter`:

```go
views := map[router.View]viewEntry{
    router.SessionListView: &viewAdapter[sessionlist.Model]{model: sessionlist.New()},
    router.SessionFormView: &viewAdapter[sessionform.Model]{model: sessionform.New()},
    router.TodoListView:    &viewAdapter[todolist.Model]{model: todolist.New()},
    router.TodoFormView:    &viewAdapter[todoform.Model]{model: todoform.New()},
    router.DebugView:       &viewAdapter[debug.Model]{model: debug.New(logPath, baseURL)},
}
r := NewRouter(views, router.SessionListView)
```

`WithInitialView` changes param type to `router.View`. `cmd/tui/main.go` updates to use `router.TodoListView`.

## Files

### New
- `internal/tui/router/router.go`

### Modified
- `internal/tui/router.go` — remove view enum, use `router.View`, accept map in NewRouter, stack-based back, simplified switch
- `internal/tui/app.go` — construct view map, pass to NewRouter, `WithInitialView(router.View)`
- `internal/tui/model.go` — update compile-time checks to use `router.View`
- `internal/tui/sessionlist/sessionlist.go` + `messages.go`
- `internal/tui/sessionform/sessionform.go` + `messages.go`
- `internal/tui/todolist/todolist.go` + `messages.go`
- `internal/tui/todoform/todoform.go` + `messages.go`
- `internal/tui/debug/debug.go` + `messages.go`
- `cmd/tui/main.go` — import router package, use `router.TodoListView`

## Verification

- `task tui:build`
- `task test`
