# UI Removals — Design

## Goal

Remove features that are no longer used, before the redesign of the remaining TUI views (`2026-10-13-tui-rest-overhaul-design.md`). This is pure deletion. Nothing that remains changes visually.

## What is removed

### Todos (the whole feature)

TUI:
- `internal/tui/todolist`, `internal/tui/todoform`
- `router.TodoListView`, `router.TodoFormView`, and their entries in `internal/tui/app.go` and `internal/tui/model.go`
- `internal/tui/provider/todosprovider.go`, its wiring in `provider.go`, and the todo calls and messages in `client.go` / `messages.go` (`fetchTodos`, `createTodo`, `deleteTodo`, `TodoCreatedMsg`, …)
- `todo_id` in the TUI's create-session request body (`client.go`), and any `todoID` parameter threaded into it
- Session list `t` key (`keys.Todos`) and its handler

CLI:
- `utena todos` and `utena new-task` (`todosCmd`, the `new-task` command, and their `AddCommand` calls in `cmd/tui/main.go`)

Daemon:
- `internal/todo` package (module, controller, router, service, store, types and tests)
- The `Todo` field, the `todo.NewTodoModule(...)` construction, and the `{"todo", "/todos", a.Todo}` mount in `internal/api/app.go`
- `Session.TodoID` (`internal/session/session.go`), `TodoID` in the create input (`types.go`, `sessionservice.go`) and in the controller mapping (`sessioncontroller.go`)

Database: the existing `todos` table and `sessions.todo_id` column are **left in place**, unused. GORM `AutoMigrate` does not drop them, and dropping them gains nothing.

### Status view

- `internal/tui/statusview`, `router.StatusView`, its entry in `app.go`
- `utena status` (the command in `cmd/tui/main.go` that starts the app on `StatusView`)
- `router.SetHelpVisible` / `SetHelpVisibleMsg` and `helpModel.visible`. The status view is their only user, so the footer becomes always visible.
- **Kept:** `utena status-line` (`cmd/tui/statusline.go`), which feeds the tuios dock cell

### Debug view

- `internal/tui/debug`, `router.DebugView`, the router's `ctrl+g` binding and `routerKeys`, and the `logPath`/`baseURL` arguments that only `debug.New` used (keep `logPath` if anything else still uses it)

### Workspace detail and workspace PR list

- `internal/tui/workspacedetail`, `internal/tui/prlist`, `router.WorkspaceDetailView`, `router.PRListView`, and their entries in `app.go` / `model.go`
- The workspace list's `enter` → detail navigation. Until the redesign lands, `enter` on a workspace does nothing (the binding is removed).
- The provider calls only those views used (e.g. `FetchPRs`), if nothing else uses them
- **Migrate to bare** is temporarily unreachable from the TUI between this PR and the redesign PR, which adds `m` on the workspace list. The daemon endpoint is unchanged.

## Out of scope

Any visual change; the redesign of workspaces, forms, pickers or session progress.

## External follow-up (outside this repo)

The README says the tuios `prefix+t` popup is configured in dotfiles (`programs/tuios/contents/config.toml`). If it runs `utena todos` or `utena new-task`, that binding must be removed or repointed when this ships. `utena status` may also be referenced there.

## Testing

- `task test` and `task lint` pass. Tests belonging to deleted packages are deleted with them. Tests elsewhere that build sessions with `TodoID` are updated.
- `grep -rni "todo\|statusview\|debugview\|workspacedetail\|prlist" internal cmd` returns no matches.
- The daemon starts against an existing database that still has the `todos` table and `todo_id` column. Covered by a migration test that creates the old `sessions` schema with a `todo_id` column, inserts a row, and runs `Migrate` on the session models.
