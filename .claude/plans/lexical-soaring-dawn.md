# Refactor View Models into Subpackages

## Context

All view models, keys, and messages currently live in the flat `internal/tui/` package. This mixes 8 distinct view models, 10 keymaps, and 5 cross-model messages into one namespace. Moving each view model into its own subpackage enforces one-model-per-file, colocates each model's keys and messages with it, and eliminates the shared `navigateMsg` / `view` enum coupling from child models.

## Package Layout

```
internal/tui/
├── app.go                    App, AppOption, WithInitialView
├── router.go                 Router, view enum, routerKeyMap
├── model.go                  ViewModel interface, compile-time checks
├── keys.go                   mergedKeyMap, appKeyMap
├── sessionlist/
│   ├── model.go              Model, sessionItem, timeAgo
│   ├── keys.go               keys
│   └── messages.go           NewSessionMsg, TodosMsg
├── sessionform/
│   ├── model.go              Model, step enum, defaultSessionName
│   ├── keys.go               formKeys, mergedKeyMap (local copy)
│   └── messages.go           BackMsg
├── todolist/
│   ├── model.go              Model, todoItem
│   ├── keys.go               keys
│   └── messages.go           NewTodoMsg, BackMsg
├── todoform/
│   ├── model.go              Model, step enum
│   ├── keys.go               formKeys, inputKeys
│   └── messages.go           BackMsg, DoneMsg
├── workspacepicker/
│   ├── model.go              Model, workspaceItem, AbbreviatePath
│   ├── keys.go               keys
│   └── messages.go           SelectedMsg, AddDirectoryMsg
├── branchpicker/
│   ├── model.go              Model, branchItem
│   ├── keys.go               keys
│   └── messages.go           SelectedMsg
├── filepicker/
│   ├── model.go              Model (aliases bubbles/filepicker as bubblefp)
│   ├── keys.go               keys
│   └── messages.go           DirectorySelectedMsg
├── debug/
│   ├── model.go              Model
│   ├── keys.go               keys
│   └── messages.go           BackMsg
└── provider/                 (unchanged)
```

## Design Decisions

### No circular dependencies

Subpackages never import the parent `tui` package. All cross-package communication happens through exported message types defined in the producing package.

### Navigation without `navigateMsg`

Currently all models emit `navigateMsg{target: someView}`, coupling them to the `view` enum in router.go. Instead, each model defines its own navigation message types:

| Model | Messages | Replaces |
|-------|----------|----------|
| sessionlist | `NewSessionMsg{}`, `TodosMsg{}` | `navigateMsg{target: sessionFormView}`, `navigateMsg{target: TodoListView}` |
| sessionform | `BackMsg{}` | `navigateMsg{target: sessionListView}` |
| todolist | `NewTodoMsg{}`, `BackMsg{}` | `navigateMsg{target: todoFormView}`, `navigateMsg{target: sessionListView}` |
| todoform | `BackMsg{}`, `DoneMsg{}` | `navigateMsg{target: TodoListView}` (two usages) |
| debug | `BackMsg{}` | `navigateMsg{target: backView}` |

The Router handles all of these in its Update, mapping them to internal view switches.

### Picker messages move to picker packages

| Current (tui package) | New location |
|----------------------|--------------|
| `workspaceSelectedMsg` | `workspacepicker.SelectedMsg` |
| `addDirectoryRequestMsg` | `workspacepicker.AddDirectoryMsg` |
| `branchSelectedMsg` | `branchpicker.SelectedMsg` |
| `directorySelectedMsg` | `filepicker.DirectorySelectedMsg` |

Forms import picker packages and handle these exported message types.

### Naming convention

Types are named `Model` within each package (e.g., `sessionlist.Model`). Constructors are `New()` (e.g., `sessionlist.New()`).

### Shared utilities

- **`mergedKeyMap`**: stays in parent `tui/keys.go` (used by App and Router). A local copy is defined in `sessionform/keys.go` (the only subpackage that needs it).
- **`AbbreviatePath`**: moves to `workspacepicker/model.go` as exported function. Forms import it from there (they already import workspacepicker).
- **`timeAgo`**: moves to `sessionlist/model.go` (only consumer).
- **`formKeys`**: defined independently in `sessionform/keys.go` and `todoform/keys.go` (identical bindings, no shared import needed).

### ViewModel interface

Stays in `tui/model.go`. Compile-time checks import subpackages:

```go
var (
    _ ViewModel[Router]               = Router{}
    _ ViewModel[sessionlist.Model]    = sessionlist.Model{}
    _ ViewModel[sessionform.Model]    = sessionform.Model{}
    // ...
)
```

## Router Changes

```go
type Router struct {
    sessionList  sessionlist.Model
    sessionForm  sessionform.Model
    todoList     todolist.Model
    todoForm     todoform.Model
    debug        debug.Model
    activeView   view
    previousView view
}
```

Replace `navigateMsg` handling with per-model message handling:

```go
case sessionlist.NewSessionMsg:
    return r.navigateTo(sessionFormView)
case sessionlist.TodosMsg:
    return r.navigateTo(TodoListView)
case sessionform.BackMsg:
    return r.navigateTo(sessionListView)
case todolist.NewTodoMsg:
    return r.navigateTo(todoFormView)
case todolist.BackMsg:
    return r.navigateTo(sessionListView)
case todoform.BackMsg, todoform.DoneMsg:
    return r.navigateTo(TodoListView)
case debug.BackMsg:
    r.activeView = r.previousView
    return r, nil
```

### keys.go

Only `mergedKeyMap` + `appKeyMap` remain (~30 lines). All other keymaps move to subpackages.

## Files

### New (8 subpackages × 3 files = 24 files)
Listed in package layout above.

### Modified
- `router.go` — import subpackages, handle subpackage message types
- `model.go` — compile-time checks import subpackages
- `keys.go` — remove all keymaps except mergedKeyMap + appKeyMap

### Deleted
- `messages.go`, `util.go`, `sessionlist.go`, `session_form.go`, `todolist.go`, `todo_form.go`, `workspace_picker.go`, `branchpicker.go`, `filepicker.go`, `debug.go`

## Steps

1. Create all 8 subpackage directories with model.go, keys.go, messages.go
2. Update `keys.go` — keep only mergedKeyMap + appKeyMap
3. Update `model.go` — compile-time checks reference subpackage types
4. Update `router.go` — import subpackages, replace navigateMsg with per-model messages
5. Delete `messages.go`, `util.go`, and all old model files
6. Update `app.go` if needed (view enum stays in router.go, WithInitialView stays)

## Verification

- `task tui:build` compiles
- `task test` passes
