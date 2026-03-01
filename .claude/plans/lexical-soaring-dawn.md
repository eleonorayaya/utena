# TUI Model Tree Restructuring

## Context

The `App` struct holds all 8 child models flat, manages all navigation state, handles session creation pipeline state, and routes every message type in a 180-line `Update()`. Data like `currentWorkspaceID` is duplicated and flows awkwardly between models. Key bindings are scattered package-level vars with no hierarchy, and help rendering is ad-hoc.

The goal: introduce **provider models** that own data and handle API lifecycle, **feature views** that own only UI state and consume provider data reactively, a **hierarchical keymap system**, and a dramatically simplified `App`.

## Architecture

### Provider Pattern

Providers own canonical data and handle all API fetch/mutation logic. They communicate with consumers via messages:

```
Provider stores data
  → emits <Name>StateUpdatedMsg (tea.Cmd wrapping a tea.Msg)
  → App routes msg to active view
  → View caches data snapshot locally for rendering

View initializes
  → emits Request<Name>StateMsg
  → App routes to provider
  → Provider re-emits current StateUpdatedMsg
  → View gets cached copy

View user action (e.g. delete)
  → emits intent msg (e.g. deleteSessionMsg)
  → App routes to provider
  → Provider handles API call → re-fetches → emits StateUpdatedMsg
```

### Message Routing in App.Update

App routes ALL messages to ALL providers and the active view. Providers and views ignore messages they don't handle:

```go
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmds []tea.Cmd

    // 1. App-level handling
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        // store, forward to all views
    case tea.KeyMsg:
        if msg.String() == "ctrl+c" { return a, tea.Quit }
        // Global keys: ? (debug toggle)
        // Dynamic keys based on activeView:
        switch a.activeView {
        case sessionListView:
            // n → new session form, t → todo list
        case todoListView:
            // n → new todo form, esc → session list
        // Forms handle esc internally (prev step / cancel)
        }
    case pipeSentMsg:
        return a, tea.Quit
    // Form cancel messages:
    case sessionFormCancelledMsg:
        a.activeView = sessionListView
        return a, a.sessionList.Init()
    case todoFormCancelledMsg:
        a.activeView = todoListView
        return a, a.todoList.Init()
    }

    // 2. Route to all providers (they ignore irrelevant messages)
    a.sessions, cmd = a.sessions.Update(msg)
    cmds = append(cmds, cmd)
    a.workspaces, cmd = a.workspaces.Update(msg)
    cmds = append(cmds, cmd)
    a.todos, cmd = a.todos.Update(msg)
    cmds = append(cmds, cmd)

    // 3. Route to active view
    switch a.activeView {
    case sessionListView:
        a.sessionList, cmd = a.sessionList.Update(msg)
    case sessionFormView:
        a.sessionForm, cmd = a.sessionForm.Update(msg)
    case todoListView:
        a.todoList, cmd = a.todoList.Update(msg)
    case todoFormView:
        a.todoForm, cmd = a.todoForm.Update(msg)
    }
    cmds = append(cmds, cmd)

    return a, tea.Batch(cmds...)
}
```

### Inter-Provider Communication

SessionsProvider determines the active workspace from the attached session and tells WorkspacesProvider via a command:

```
sessionsLoadedMsg → SessionsProvider
  → stores sessions
  → emits sessionsStateUpdatedMsg (for views)
  → emits setActiveWorkspaceMsg{workspaceID} (for WorkspacesProvider)

setActiveWorkspaceMsg → WorkspacesProvider
  → stores activeWorkspaceID
  → emits workspacesStateUpdatedMsg (for views that care about active workspace)
```

## New Model Tree

```
App
├── Providers (data + API lifecycle)
│   ├── sessions: SessionsProvider
│   ├── workspaces: WorkspacesProvider
│   └── todos: TodosProvider
│
├── Views (UI state only, consume provider data via *StateUpdatedMsg)
│   ├── sessionList: SessionListModel     — browse/activate/delete sessions
│   ├── sessionForm: SessionFormModel     — multi-step session creation wizard
│   ├── todoList: TodoListModel           — browse/delete todos
│   └── todoForm: TodoFormModel           — multi-step todo creation wizard
│
├── help: help.Model
├── activeView, width, height, logPath, initialView
```

### SessionsProvider (`internal/tui/sessions_provider.go`)

```go
type SessionsProvider struct {
    sessions       []session.Session
    claudeSessions map[string][]claude.ClaudeSession
}
```

**Owns**: session data, claude session data
**Handles**:
- `sessionsLoadedMsg` → store, emit `sessionsStateUpdatedMsg` + `setActiveWorkspaceMsg`
- `claudeSessionsLoadedMsg` → store, emit `sessionsStateUpdatedMsg`
- `requestSessionsStateMsg` → re-emit `sessionsStateUpdatedMsg` with current data
- `activateSessionMsg{name}` → call `activateSession()` API → on success emit `sessionActivatedMsg{name, pipeCommand: "switch_session"}`
- `createSessionMsg{name, workspaceID, branch, workspacePath}` → call `createSession()` API → on success call `activateSession()` → on success emit `sessionActivatedMsg{name, pipeCommand: "create_session", workspacePath}` (uses closure to carry form values through async chain, no persistent pending state)
- `sessionActivatedMsg` → call `sendZellijPipe(pipeCommand, name, workspacePath)`
- `deleteSessionMsg` → call `deleteSession()` API
- `sessionDeletedMsg` → re-fetch sessions

**No pending state**: The session creation async pipeline uses closures in `tea.Cmd` functions to carry form values (`name`, `workspacePath`) through the create → activate → pipe chain. Each step's command captures what the next step needs.

**Emits**: `sessionsStateUpdatedMsg{sessions, claudeSessions}`, `setActiveWorkspaceMsg{workspaceID}`

### WorkspacesProvider (`internal/tui/workspaces_provider.go`)

```go
type WorkspacesProvider struct {
    workspaces        []workspace.Workspace
    activeWorkspaceID string
}
```

**Owns**: workspace list, active workspace ID (canonical source)
**Handles**:
- `workspacesLoadedMsg` → store, emit `workspacesStateUpdatedMsg`
- `requestWorkspacesStateMsg` → re-emit `workspacesStateUpdatedMsg` with current data
- `setActiveWorkspaceMsg` → store activeWorkspaceID, emit `workspacesStateUpdatedMsg`
- `addWorkspaceMsg` → call `addWorkspace()` API
- `workspaceAddedMsg` → re-fetch workspaces

**Emits**: `workspacesStateUpdatedMsg{workspaces, activeWorkspaceID}`

### TodosProvider (`internal/tui/todos_provider.go`)

```go
type TodosProvider struct {
    todos []todo.Todo
}
```

**Owns**: todo data
**Handles**:
- `todosLoadedMsg` → store, emit `todosStateUpdatedMsg`
- `requestTodosStateMsg` → re-emit `todosStateUpdatedMsg` with current data
- `createTodoMsg` → call `createTodo()` API
- `todoCreatedMsg` → re-fetch todos
- `deleteTodoMsg` → call `deleteTodo()` API
- `todoDeletedMsg` → re-fetch todos

**Emits**: `todosStateUpdatedMsg{todos}`

### SessionListModel (`internal/tui/sessionlist.go` — modified)

Top-level view for browsing sessions.

**On Init**: emits `requestSessionsStateMsg`
**On `sessionsStateUpdatedMsg`**: caches sessions + claude data, rebuilds list items
**Keys**: enter (select), d (close)
**Produces**: `activateSessionMsg`, `deleteSessionMsg`

### SessionFormModel (`internal/tui/session_form.go` — new)

Multi-step session creation wizard. Owns the entire workspace → branch → name flow and accumulates form state across steps. Uses shared, context-agnostic child models. Also owns a file picker for the "add workspace directory" sub-flow.

```go
type SessionFormModel struct {
    activeStep        sessionFormStep // workspacePicker | filePicker | dirTypeChoice | branchPicker | nameInput
    workspacePicker   WorkspacePickerModel  // shared, context-agnostic
    filePicker        FilePickerModel       // directory browser, context-agnostic
    branchPicker      BranchPickerModel
    nameInput         textinput.Model       // direct use, no wrapper
    selectedWorkspace workspace.Workspace
    selectedBranch    string
    selectedDirPath   string                // from file picker, pending workspace-or-root choice
    nameErr           string
    width, height     int
}
```

**On Init**: emits `requestWorkspacesStateMsg`
**On `workspacesStateUpdatedMsg`**: forwards to workspace picker
**On `workspaceSelectedMsg`**: if git repo → advance to branch picker; else → advance to name input
**On `directorySelectedMsg`**: store path, advance to dirTypeChoice step (w: workspace, r: root)
**On dirTypeChoice**: emit `addWorkspaceMsg{path, asRoot}` → on `workspacesStateUpdatedMsg` return to workspace picker
**On `branchSelectedMsg`**: store branch, advance to name input
**Name input handling**: form handles enter key directly — validates session name, emits `createSessionMsg{name, workspaceID, branch, workspacePath}` with all accumulated values
**Back at any step**: go to previous step; at first step emit `sessionFormCancelledMsg`
**Produces**: `createSessionMsg` (complete), `addWorkspaceMsg`, `sessionFormCancelledMsg`

### TodoListModel (`internal/tui/todolist.go` — modified)

Top-level view for browsing todos.

**On Init**: emits `requestTodosStateMsg` + `requestWorkspacesStateMsg`
**On `todosStateUpdatedMsg`**: caches todos, rebuilds list items
**On `workspacesStateUpdatedMsg`**: caches `activeWorkspaceID` for workspace filtering
**Keys**: d (delete), a (all/current)
**Produces**: `deleteTodoMsg`

### TodoFormModel (`internal/tui/todo_form.go` — new)

Multi-step todo creation wizard. Uses shared, context-agnostic child models. Also owns a file picker for "add workspace directory" sub-flow.

```go
type TodoFormModel struct {
    activeStep        todoFormStep // workspacePicker | filePicker | dirTypeChoice | nameInput
    workspacePicker   WorkspacePickerModel  // shared, context-agnostic
    filePicker        FilePickerModel       // directory browser, context-agnostic
    nameInput         textinput.Model       // direct use
    descInput         textinput.Model       // direct use
    focusIndex        int
    selectedWorkspace *workspace.Workspace
    selectedDirPath   string                // from file picker, pending workspace-or-root choice
    activeWorkspaceID string                // cached for sorting workspace picker
    nameErr           string
    width, height     int
}
```

**On Init**: emits `requestWorkspacesStateMsg`
**On `workspacesStateUpdatedMsg`**: forwards to workspace picker (sorts active workspace first)
**On `workspaceSelectedMsg`**: store workspace, advance to name/desc inputs
**On `directorySelectedMsg`**: store path, advance to dirTypeChoice step
**On dirTypeChoice**: emit `addWorkspaceMsg{path, asRoot}` → on `workspacesStateUpdatedMsg` return to workspace picker
**Name input handling**: form handles enter key directly — validates name, emits `createTodoMsg{name, description, workspaceID}`
**Back at any step**: go to previous step; at first step emit `todoFormCancelledMsg`
**Produces**: `createTodoMsg` (complete), `addWorkspaceMsg`, `todoFormCancelledMsg`

### Shared, Context-Agnostic UI Models

**WorkspacePickerModel** (`internal/tui/workspace_picker.go` — new, replaces both `newsession.go` and `todoworkspacepicker.go`)
- Shows a list of workspaces. That's it.
- Handles `workspacesStateUpdatedMsg` to populate list
- On select → emits `workspaceSelectedMsg{workspace}` — parent form decides what it means
- On `a` → emits `addDirectoryRequestMsg` — parent form switches to file picker step
- Has `Keys()`: enter (select), a (add dir), esc (back)
- Can be configured with optional sort (e.g., active workspace first) via constructor param

**BranchPickerModel** (`internal/tui/branchpicker.go` — simplified)
- Shows a list of branches
- On select → emits `branchSelectedMsg{branch}` — parent handles
- Has `Keys()`: enter (select)

**FilePickerModel** (`internal/tui/filepicker.go` — simplified)
- Only picks a directory. Emits `directorySelectedMsg{path}` when user confirms.
- The `choosingType` phase (workspace vs root) moves OUT of the file picker to the parent form model as a `dirTypeChoice` step.
- Has `Keys()`: s (select dir), . (hidden), esc (back)

## Message Types

### Provider state messages
| Message | Producer | Consumer |
|---------|----------|----------|
| `sessionsStateUpdatedMsg{sessions, claudeSessions}` | SessionsProvider | SessionListModel |
| `workspacesStateUpdatedMsg{workspaces, activeWorkspaceID}` | WorkspacesProvider | SessionFormModel, TodoListModel, TodoFormModel |
| `todosStateUpdatedMsg{todos}` | TodosProvider | TodoListModel |

### State request messages
| Message | Producer | Consumer |
|---------|----------|----------|
| `requestSessionsStateMsg` | SessionListModel (on Init) | SessionsProvider |
| `requestWorkspacesStateMsg` | SessionFormModel, TodoFormModel (on Init) | WorkspacesProvider |
| `requestTodosStateMsg` | TodoListModel (on Init) | TodosProvider |

### Inter-provider messages
| Message | Producer | Consumer |
|---------|----------|----------|
| `setActiveWorkspaceMsg{workspaceID}` | SessionsProvider | WorkspacesProvider |

### Intent messages (view → provider)
| Message | Producer | Consumer |
|---------|----------|----------|
| `activateSessionMsg{name}` | SessionListModel | SessionsProvider |
| `createSessionMsg{name, workspaceID, branch, workspacePath}` | SessionFormModel | SessionsProvider |
| `deleteSessionMsg` | SessionListModel | SessionsProvider |
| `createTodoMsg{name, description, workspaceID}` | TodoFormModel | TodosProvider |
| `deleteTodoMsg` | TodoListModel | TodosProvider |
| `addWorkspaceMsg{path, asRoot}` | SessionFormModel, TodoFormModel (after directorySelectedMsg + type choice) | WorkspacesProvider |


### Navigation messages
Top-level navigation (`n`, `t`, `esc` on lists) is handled by App's dynamic key handling — App directly switches views. Forms emit cancel messages when the user backs out from the first step:

| Message | Producer | Consumer |
|---------|----------|----------|
| `sessionFormCancelledMsg` | SessionFormModel (esc at first step) | App (→ session list) |
| `todoFormCancelledMsg` | TodoFormModel (esc at first step) | App (→ todo list) |
| `pipeSentMsg` | SessionsProvider | App (quit) |

### Form-internal messages (child → parent form)
| Message | Producer | Consumer |
|---------|----------|----------|
| `workspaceSelectedMsg{workspace}` | WorkspacePickerModel | SessionFormModel, TodoFormModel |
| `branchSelectedMsg{branch}` | BranchPickerModel | SessionFormModel |
| `addDirectoryRequestMsg` | WorkspacePickerModel | SessionFormModel, TodoFormModel (switches to file picker step) |
| `directorySelectedMsg{path}` | FilePickerModel | SessionFormModel, TodoFormModel (switches to dirTypeChoice step) |

### Internal API messages (provider-internal, not seen by views)
`sessionsLoadedMsg`, `claudeSessionsLoadedMsg`, `workspacesLoadedMsg`, `todosLoadedMsg`, `sessionActivatedMsg`, `sessionDeletedMsg`, `todoCreatedMsg`, `todoDeletedMsg`, `workspaceAddedMsg` — all handled within their respective provider. Views never need to handle these.

Note: `sessionCreatedMsg` is eliminated as a separate type. The create → activate → pipe chain is handled entirely within closure-captured `tea.Cmd` functions in the provider.

## Keymap & Help Architecture

Every model that handles key input defines `Keys() help.KeyMap`. Parent models merge their keys with the active child's. App renders a single help bar using the merged keymap from the entire active hierarchy.

```go
type mergedKeyMap struct {
    keymaps []help.KeyMap
}

func (m mergedKeyMap) ShortHelp() []key.Binding { /* concatenate all */ }
func (m mergedKeyMap) FullHelp() [][]key.Binding { /* concatenate all */ }
```

**Key handling flow**: App checks global keys → if no match, passes to active view → view checks its keys → if no match, passes to active child → leaf model handles.

**Help rendering**: `App.View()` always renders `content + help.View(mergedKeys)`. Disable `list.Model` built-in help via `SetShowHelp(false)`. Each model's `Keys()` returns only its custom action keys.

| Model | Keys |
|-------|------|
| App (always) | ctrl+c (quit), ? (debug) |
| App (dynamic, on session list) | n (new session), t (todos) |
| App (dynamic, on todo list) | n (new todo), esc (back to sessions) |
| SessionListModel | enter (select), d (close) |
| TodoListModel | d (delete), a (all/current) |
| SessionFormModel | esc (prev step; at first step emits `sessionFormCancelledMsg`), delegates to active step's Keys(); at name step: enter (submit) |
| TodoFormModel | esc (prev step; at first step emits `todoFormCancelledMsg`), delegates to active step's Keys(); at name step: enter (submit), tab (next field) |
| WorkspacePickerModel (shared) | enter (select), a (add dir) |
| BranchPickerModel | enter (select) |
| FilePickerModel | s (select dir), . (hidden) |

App's `Keys()` method is dynamic — it returns different bindings based on `activeView`. This means the help bar automatically shows the correct keys for the current context. The `n` key maps to "new session" or "new todo" depending on which list is active. The `t` key is only included when on the session list. The `esc` key on list views is App-level (todo list → session list). On form views, `esc` is handled by the form (prev step or cancel) — when the form cancels (first step esc), it emits a cancel message that App handles to return to the parent list.

## File Changes

### New files
- `internal/tui/sessions_provider.go` — SessionsProvider
- `internal/tui/workspaces_provider.go` — WorkspacesProvider
- `internal/tui/todos_provider.go` — TodosProvider
- `internal/tui/session_form.go` — SessionFormModel (multi-step wizard, uses textinput.Model directly)
- `internal/tui/todo_form.go` — TodoFormModel (multi-step wizard, uses textinput.Model directly)
- `internal/tui/workspace_picker.go` — WorkspacePickerModel (shared, context-agnostic, replaces newsession.go and todoworkspacepicker.go)

### Modified files
- `internal/tui/app.go` — simplified to providers + top-level views + routing. Handles navigation messages from views. Global keys: ctrl+c, debug.
- `internal/tui/keys.go` — add `mergedKeyMap` helper, restructure into per-model keymap structs
- `internal/tui/sessionlist.go` — handle `sessionsStateUpdatedMsg`, add `Keys()`, disable list help. Remove n/t keys (moved to App dynamic keymap). Keep enter/d.
- `internal/tui/branchpicker.go` — simplify to emit `branchSelectedMsg{branch}` (no workspace), add `Keys()`, disable list help
- `internal/tui/todolist.go` — handle `todosStateUpdatedMsg` + `workspacesStateUpdatedMsg`, remove `currentWorkspaceID`, add `Keys()`, disable list help. Remove n/esc keys (moved to App dynamic keymap). Keep d/a.
- `internal/tui/filepicker.go` — remove `choosingType` phase (moves to form models), just emit `directorySelectedMsg{path}`, add `Keys()`
- `internal/tui/client.go` — `fetchTodoData()` becomes `fetchTodos()` (no longer fetches sessions)

### Deleted files
- `internal/tui/newsession.go` — replaced by shared `workspace_picker.go`
- `internal/tui/todoworkspacepicker.go` — replaced by shared `workspace_picker.go`
- `internal/tui/nameinput.go` — form models use `textinput.Model` directly
- `internal/tui/newtodo.go` — TodoFormModel uses `textinput.Model` directly

### Unchanged
- `internal/tui/util.go`

## Implementation Steps

### Step 1: Message types and keymap infrastructure
- Define all new message types: provider state (`*StateUpdatedMsg`), request state (`request*StateMsg`), inter-provider (`setActiveWorkspaceMsg`), generic UI (`workspaceSelectedMsg`, `branchSelectedMsg`, `directorySelectedMsg`), intent, navigation
- Define `mergedKeyMap` helper struct
- Restructure key bindings into per-model keymap structs implementing `help.KeyMap`

### Step 2: Create providers
- `SessionsProvider` — session lifecycle (fetch, CRUD, create→activate→pipe chain via closures)
- `WorkspacesProvider` — workspace fetch/add, activeWorkspaceID management
- `TodosProvider` — todo fetch/CRUD
- Each handles its data messages + `request*StateMsg` + intent messages
- Update `client.go`: `fetchTodoData()` → `fetchTodos()`

### Step 3: Create shared WorkspacePickerModel
- Context-agnostic list of workspaces
- Handles `workspacesStateUpdatedMsg` to populate list
- Emits `workspaceSelectedMsg{workspace}` on selection
- Constructor accepts optional sort config (e.g., prioritize active workspace)
- Add `Keys()`, disable list help

### Step 4: Create form models
- `SessionFormModel` — workspace picker → branch picker → name textinput. Accumulates form state. Emits complete `createSessionMsg` on submit.
- `TodoFormModel` — workspace picker → name/desc textinputs. Emits complete `createTodoMsg` on submit.
- Both emit `requestWorkspacesStateMsg` on init, forward `workspacesStateUpdatedMsg` to picker
- Add `Keys()` that delegates to active step

### Step 5: Refactor list models
- `SessionListModel` — handle `sessionsStateUpdatedMsg`, emit `requestSessionsStateMsg` on init, remove `n`/`t` keys (moved to App), add `Keys()`, disable list help
- `TodoListModel` — handle `todosStateUpdatedMsg` + `workspacesStateUpdatedMsg`, emit `requestTodosStateMsg` + `requestWorkspacesStateMsg` on init, remove `currentWorkspaceID` field, remove `n`/`esc` keys (moved to App), add `Keys()`, disable list help
- `BranchPickerModel` — simplify to emit `branchSelectedMsg{branch}` (no workspace), add `Keys()`, disable list help

### Step 6: Refactor FilePickerModel
- Remove `choosingType` phase (moves to form models as `dirTypeChoice` step)
- Just pick a directory and emit `directorySelectedMsg{path}`
- Add `Keys()`

### Step 7: Refactor App
- Fields: 3 providers + 4 top-level views (sessionList, sessionForm, todoList, todoForm) + help
- `Update()`: app-level handling (ctrl+c quit, debug toggle, dynamic routing keys based on activeView) → handle form cancel msgs → route to providers → route to active view
- `View()` renders active view content + merged help bar
- `Keys()` returns global keys (quit, debug) + dynamic keys based on activeView (n, t, esc)

### Step 8: Delete obsolete files and clean up
- Delete `newsession.go`, `todoworkspacepicker.go`, `nameinput.go`, `newtodo.go`
- Remove unused message types and dead code

## Verification

1. `task build` — compiles
2. Manual testing with `task tui:run`:
   - Session list loads, claude status indicators show
   - Create session flow: workspace → branch → name → activate + pipe
   - Delete session (double-press d)
   - Switch to todos (t) and back (esc)
   - Todo list loads, workspace filtering works
   - Create todo flow: n → workspace → form → enter
   - Delete todo (double-press d)
   - File picker from both session and todo workspace pickers
   - Adding workspace refreshes the picker
   - Debug view (?) and back (esc)
   - ctrl+c quits from any view
   - Help bar always visible, updates per-view
   - Window resize propagates
