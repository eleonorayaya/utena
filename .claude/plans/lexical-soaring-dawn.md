# Extract Router Model from App

## Context

App currently mixes two responsibilities: wiring providers with data and routing views. The `activeView`, `previousView`, view dispatch switches, `onNavigate`, `onWindowSizeMsg`, `View()`, and `keys()` are all routing concerns that should live in their own model. This will leave App as a thin shell that wires providers to a router.

## Design

### Router (`internal/tui/router.go` — new)

Owns all view models and all view-routing state/logic.

```go
type Router struct {
    sessionList  SessionListModel
    sessionForm  SessionFormModel
    todoList     TodoListModel
    todoForm     TodoFormModel
    debug        DebugModel
    activeView   view
    previousView view
}
```

**Moves from app.go to router.go:**
- `view` type + all constants (`sessionListView` ... `backView`)
- `navigateMsg` handling (`onNavigate`)
- `?` key handling (debug toggle — it's view switching, not app-level like ctrl+c)
- `onWindowSizeMsg` (routes to all views)
- Active view dispatch in Update (the `switch a.activeView` block)
- `View()` — returns active view's rendered content
- `Keys()` — returns active view's keymap

**Router.Update flow:**
1. `tea.WindowSizeMsg` → forward to all views
2. `tea.KeyMsg` with `?` → toggle debug view
3. `navigateMsg` → `onNavigate`
4. Everything else → route to active view

### App after refactor

```go
type App struct {
    sessions   SessionsProvider
    workspaces WorkspacesProvider
    todos      TodosProvider
    router     Router
    help       help.Model
    initialView view
    width      int
    height     int
}
```

**App.Update flow:**
1. `tea.WindowSizeMsg` → store full dimensions, create adjusted msg (height-2), pass to router
2. `tea.KeyMsg` ctrl+c → quit
3. Everything else → providers + router

**App.View:** `router.View()` + help bar

**App.keys():** `appKeys` (just quit) + `router.Keys()`

### appKeys change

`appKeys` keeps only `Quit`. `Debug` moves to a router-level key since it's view switching. The help bar merges `appKeys` + `router.Keys()`, so `?` still appears.

## Files

### New
- `internal/tui/router.go` — Router struct, `NewRouter`, `Update`, `onNavigate`, `onWindowSizeMsg`, `View`, `Keys`

### Modified
- `internal/tui/app.go` — remove view type/constants, remove all view models, remove `previousView`, remove `onNavigate`/`onWindowSizeMsg`/`debugViewContent`/`keys` view-switch logic. Add `router Router` field. Simplify Update/View/keys.
- `internal/tui/keys.go` — move `Debug` out of `appKeyMap` into a new `routerKeyMap` (or just handle it inline in Router)

### Unchanged
- All view models, providers, messages.go, client.go, debug.go — they already emit `navigateMsg` and don't reference App directly

## Steps

1. Create `internal/tui/router.go` with the `view` type/constants, `Router` struct, `NewRouter`, and all methods (`Update`, `onNavigate`, `onWindowSizeMsg`, `View`, `Keys`)
2. Update `internal/tui/keys.go` — `appKeyMap` keeps only `Quit`; add `Debug` key binding to Router (either as a `routerKeyMap` or a package-level var)
3. Update `internal/tui/app.go` — replace view models + routing state with `router Router`, simplify Update/View/keys, adjust `NewApp`/`WithInitialView`

## Verification

- `task tui:build` compiles
- `task test` passes
