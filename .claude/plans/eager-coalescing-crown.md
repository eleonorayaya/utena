# Workspace UI Views

## Context

The TUI currently only navigates between sessions, todos, and a status view. There's no way to browse workspaces or their associated PRs. This adds three new views: a workspace list, workspace detail page, and PR list — giving visibility into workspace state and PR activity from the TUI.

## Navigation Flow

```
SessionListView (press "w") → WorkspaceListView (press enter) → WorkspaceDetailView (press "p") → PRListView
                                                                  ↑ shows: name, path, repo, last used
```

Each level uses `esc` to go back via the router stack.

## Tasks

### Task 1: Join Repo on Workspace and add PR endpoint

Add a `Repo *git.Repo` association to the Workspace model (GORM `foreignKey:RepoID`), so workspace queries can join the repo table and return nested repo data. Also add the PR list endpoint.

**Files:**
- `internal/workspace/workspace.go` — add `Repo *git.Repo` field with `json:"repo,omitempty" gorm:"foreignKey:RepoID"`
- `internal/workspace/workspacestore.go` — add `.Joins("Repo")` to `GetByID`, `GetByPath`, `GetByRepoID`, and `List` queries so the repo is eagerly loaded
- `internal/workspace/workspacerouter.go` — add `r.Get("/{id}/prs", wr.controller.ListPRs)`
- `internal/workspace/workspacecontroller.go` — add `ListPRs` handler: parse ID, get workspace, check `RepoID != nil`, read optional `?state=` query param, call `gitService.SearchPRs(*ws.RepoID, state)` (queries local DB only — PRs are synced by the background `git.prs` job), return JSON
- `internal/workspace/types.go` — add `PRListResponse{PullRequests []git.PullRequest}`

### Task 2: Router view constants

**File:** `internal/tui/router/view.go`

Add after `StatusView`:
```go
WorkspaceListView
WorkspaceDetailView
PRListView
```

### Task 3: Provider PR fetching

Extend existing `workspacesProvider` (not a new provider).

**Files:**
- `internal/tui/provider/client.go` — add `fetchPRs(workspaceID uint, state string)` method, `GET /workspaces/{id}/prs?state={state}`, returns `prsLoadedMsg`
- `internal/tui/provider/workspacesprovider.go` — add:
  - `PRsStateUpdatedMsg{PullRequests []git.PullRequest, WorkspaceID uint}` (public)
  - `FetchPRs(workspaceID uint, state string) tea.Cmd` (public)
  - Internal intent/loaded msgs, handled in `Update()`

### Task 4: WorkspaceListView

**New package:** `internal/tui/workspacelist/`

Follows `todolist` pattern exactly.

- `workspacelist.go` — Model with `list.Model`, fetches workspaces on Init, rebuilds items on `WorkspacesStateUpdatedMsg`. Enter navigates to detail via `tea.Sequence(router.NavigateTo(WorkspaceDetailView), workspacedetail.Select(ws))`
- `workspaceitem.go` — Title: name. Description: path (abbreviated with `~`) + git indicator
- `keys.go` — Select (enter), Back (esc)

### Task 5: WorkspaceDetailView

**New package:** `internal/tui/workspacedetail/`

Follows `sessionprogress` pattern (non-list, static detail page with styled text).

- `model.go` — Model stores `*workspace.Workspace` (which now includes nested `Repo`). Receives `SelectMsg` from workspace list. Renders name, path, git repo full name (from `ws.Repo.FullName`), last used. Press `p` navigates to PR list if workspace has a repo. Press `esc` goes back.
- `messages.go` — `SelectMsg{Workspace}`, `Select(ws) tea.Cmd`
- `keys.go` — PRs (p), Back (esc)

### Task 6: PRListView

**New package:** `internal/tui/prlist/`

Follows `todolist` list pattern.

- `prlist.go` — Model with `list.Model`. Receives `SelectMsg{WorkspaceID}` from detail view, triggers `provider.FetchPRs(wsID, "open")`. Rebuilds items on `PRsStateUpdatedMsg`. Press `o` opens PR in browser.
- `pritem.go` — Title: `#123 Title [draft] [assigned]`. Description: `@author · open`
- `messages.go` — `SelectMsg{WorkspaceID}`, `Select(wsID) tea.Cmd`
- `keys.go` — Open (o), Back (esc)

### Task 7: Wire views into app

**File:** `internal/tui/app.go`

Import new packages, add to views map:
```go
router.WorkspaceListView:   &router.ViewAdapter[workspacelist.Model]{Model: workspacelist.New()},
router.WorkspaceDetailView: &router.ViewAdapter[workspacedetail.Model]{Model: workspacedetail.New()},
router.PRListView:          &router.ViewAdapter[prlist.Model]{Model: prlist.New()},
```

### Task 8: Add "w" keybinding to session list

**Files:**
- `internal/tui/sessionlist/keys.go` — add `Workspaces` binding (`w`, "workspaces")
- `internal/tui/sessionlist/sessionlist.go` — in `OnKeyMsg`, add case for `keys.Workspaces` → `router.NavigateTo(router.WorkspaceListView)`

## Execution Order

1. Task 1 (API + model) + Task 2 (router constants) — parallel, no dependencies
2. Task 3 (provider) — depends on Task 1
3. Tasks 4, 5, 6, 8 — can parallelize once Tasks 2 and 3 are done
4. Task 7 (wiring) — last, depends on all views

## Key Patterns to Follow

- **List views** (`todolist`): `ulist.New("Title")`, `rebuildItems()`, `OnKeyMsg` with filter guard
- **Detail views** (`sessionprogress`): `StartMsg`/`SelectMsg` pattern, lipgloss styled text, `SetSize`
- **Data passing**: `tea.Sequence(router.NavigateTo(view), targetview.Select(data))`
- **Provider**: intent msg → client fetch → loaded msg → emitState → public state msg
- **Keys**: `keyMap` struct, `ShortHelp`/`FullHelp`, `var _ help.KeyMap = keys`

## Verification

1. `task test` — all existing tests pass
2. `task daemon:run` — start daemon
3. `task tui:run` — launch TUI
4. Press `w` from session list → workspace list appears
5. Select a workspace → detail page shows name, path, repo full name
6. Press `p` → PR list loads for that workspace
7. Press `o` on a PR → opens in browser
8. Press `esc` at each level → navigates back correctly
