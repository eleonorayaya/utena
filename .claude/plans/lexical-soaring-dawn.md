# Extract Providers into Sub-Package

## Context

App currently owns three provider models (`SessionsProvider`, `WorkspacesProvider`, `TodosProvider`) and an HTTP client, all in the `tui` package alongside view models. This mixes data-layer concerns with UI concerns. Moving providers and client into `internal/tui/provider/` separates these responsibilities and establishes a clear public API boundary: views consume public state update messages and call public action functions, while all provider internals (intent messages, client response messages, HTTP implementation) stay private.

## Design

### Package: `internal/tui/provider/`

**Public surface:**

```go
// Provider interface — used as App's field type
type Provider interface {
    Update(tea.Msg) (Provider, tea.Cmd)
}

// State update messages — consumed by views via type switch
type SessionsStateUpdatedMsg struct { ... }
type WorkspacesStateUpdatedMsg struct { ... }
type TodosStateUpdatedMsg struct { ... }
type BranchesLoadedMsg struct { ... }
type TodoCreatedMsg struct{}
type ErrMsg struct{ Err error }

// Action cmd functions — called by views to trigger provider behavior
func Configure(port, pipe string)
func DaemonURL() string
func NewRootProvider() Provider

func FetchSessions() tea.Cmd
func FetchClaudeSessions() tea.Cmd
func FetchWorkspaces() tea.Cmd
func FetchBranches(workspaceID string) tea.Cmd
func FetchTodos() tea.Cmd

func RequestSessionsState() tea.Cmd
func RequestWorkspacesState() tea.Cmd

func ActivateSession(name string) tea.Cmd
func CreateSession(name, workspaceID, branch, workspacePath string) tea.Cmd
func DeleteSession(id string) tea.Cmd
func CreateTodo(name, description, workspaceID string) tea.Cmd
func DeleteTodo(id string) tea.Cmd
func AddWorkspace(path string, asRoot bool) tea.Cmd
```

**Private internals:** `rootProvider` struct, three sub-providers, all intent/request messages, all client response messages (except the public ones above), HTTP client implementation.

### Message Categorization

**Move to provider (public)** — consumed by views in type switches:
- `SessionsStateUpdatedMsg` (fields: `Sessions`, `ClaudeSessions`)
- `WorkspacesStateUpdatedMsg` (fields: `Workspaces`, `ActiveWorkspaceID`)
- `TodosStateUpdatedMsg` (fields: `Todos`)
- `BranchesLoadedMsg` (fields: `Branches`)
- `TodoCreatedMsg`
- `ErrMsg` (field: `Err`)

**Move to provider (private)** — only used within provider package:
- `sessionsLoadedMsg`, `claudeSessionsLoadedMsg`, `workspacesLoadedMsg`, `todosLoadedMsg`
- `sessionActivatedMsg`, `sessionCreatedMsg`, `sessionDeletedMsg`
- `workspaceAddedMsg`, `todoDeletedMsg`
- `requestSessionsStateMsg`, `requestWorkspacesStateMsg`, `requestTodosStateMsg`
- `createSessionIntentMsg`, `deleteSessionIntentMsg`
- `createTodoIntentMsg`, `deleteTodoIntentMsg`
- `addWorkspaceIntentMsg`, `activateSessionMsg`, `setActiveWorkspaceMsg`

**Stay in tui/messages.go** — purely view-to-view messages:
- `navigateMsg`, `workspaceSelectedMsg`, `branchSelectedMsg`
- `addDirectoryRequestMsg`, `directorySelectedMsg`

### RootProvider

```go
type rootProvider struct {
    sessions   sessionsProvider
    workspaces workspacesProvider
    todos      todosProvider
}

func (p rootProvider) Update(msg tea.Msg) (Provider, tea.Cmd) {
    var cmds []tea.Cmd
    var cmd tea.Cmd
    p.sessions, cmd = p.sessions.Update(msg)
    cmds = append(cmds, cmd)
    p.workspaces, cmd = p.workspaces.Update(msg)
    cmds = append(cmds, cmd)
    p.todos, cmd = p.todos.Update(msg)
    cmds = append(cmds, cmd)
    return p, tea.Batch(cmds...)
}
```

### App after refactor

```go
type App struct {
    provider provider.Provider
    router   Router
    help     help.Model
    width    int
    height   int
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // ctrl+c, window size handling...
    var cmds []tea.Cmd
    var cmd tea.Cmd
    a.provider, cmd = a.provider.Update(msg)
    cmds = append(cmds, cmd)
    a.router, cmd = a.router.Update(msg)
    cmds = append(cmds, cmd)
    return a, tea.Batch(cmds...)
}
```

## Files

### New (`internal/tui/provider/`)
- `provider.go` — `Provider` interface, `rootProvider` struct, `NewRootProvider`
- `messages.go` — public message types (6 types listed above)
- `actions.go` — public `tea.Cmd` functions + `Configure` + `DaemonURL`
- `client.go` — private HTTP client, private response message types, `apiClient`/`baseURL`/`pipeName` vars
- `sessions.go` — private `sessionsProvider`
- `workspaces.go` — private `workspacesProvider`
- `todos.go` — private `todosProvider`

### Modified
- `internal/tui/app.go` — replace three provider fields with single `provider.Provider`, update `NewApp`, `Init`, `Update`
- `internal/tui/messages.go` — remove all provider-related messages, keep only view-to-view messages
- `internal/tui/sessionlist.go` — use `provider.X` for message types and action functions
- `internal/tui/session_form.go` — use `provider.X`
- `internal/tui/todolist.go` — use `provider.X`
- `internal/tui/todo_form.go` — use `provider.X`
- `internal/tui/workspace_picker.go` — use `provider.WorkspacesStateUpdatedMsg`
- `internal/tui/branchpicker.go` — use `provider.BranchesLoadedMsg`
- `internal/tui/debug.go` — use `provider.DaemonURL()` instead of `baseURL`
- `cmd/tui/main.go` — call `provider.Configure(...)` instead of `tui.Configure(...)`

### Unchanged
- `internal/tui/router.go`, `keys.go`, `model.go`, `filepicker.go`, `helpers.go`

## Steps

1. Create `internal/tui/provider/` directory and all 7 files
2. Update `internal/tui/messages.go` — remove provider-related messages
3. Update all view files — import `provider` package, replace message types (e.g., `sessionsStateUpdatedMsg` → `provider.SessionsStateUpdatedMsg`, `.sessions` → `.Sessions`), replace action calls (e.g., `fetchSessions()` → `provider.FetchSessions()`, intent msg creation → `provider.CreateTodo(...)`)
4. Update `internal/tui/app.go` — single `provider.Provider` field
5. Update `internal/tui/debug.go` — `provider.DaemonURL()`
6. Update `cmd/tui/main.go` — `provider.Configure(...)`

## Verification

- `task tui:build` compiles
- `task test` passes
