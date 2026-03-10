# Robust Async Session Creation

## Context

Session creation sometimes fails (likely git pull timeout on large repos), leaving the session in a partial state — worktree created but no tmux session. The current synchronous flow blocks the TUI until all steps complete (or fail), and there's no way to recover from the failed step.

The goal is to make session creation async with step-by-step progress tracking, error capture, and repair support. Also consolidate the data model — remove redundant fields, unify status tracking, and merge the concepts of "dead" and "failed" sessions into a single "broken" status.

## Data Model Changes

### Simplified Session struct

**`internal/session/session.go`**

Remove these fields:
- `IsActive`, `IsDead`, `IsDeleted` → replaced by `Status`
- `BranchName` → redundant with `Branch` (the prefix is applied during creation and stored in `Branch`)
- `WorkspaceName` → resolved on-the-fly via `resolveWorkspaceName()` already, no need to persist
- `Cleanup` → replaced by `Resources` (unified tracking)

```go
type SessionStatus string

const (
    StatusCreating SessionStatus = "creating"
    StatusReady    SessionStatus = "ready"
    StatusBroken   SessionStatus = "broken"
    StatusDeleted  SessionStatus = "deleted"
)

type ResourceStatus string

const (
    ResourcePending  ResourceStatus = "pending"
    ResourceCreating ResourceStatus = "creating"
    ResourceReady    ResourceStatus = "ready"
    ResourceFailed   ResourceStatus = "failed"
    ResourceRemoved  ResourceStatus = "removed"
)

type ResourceState struct {
    Status ResourceStatus `json:"status"`
    Error  string         `json:"error,omitempty"`
}

type Resources struct {
    Branch   *ResourceState `json:"branch,omitempty"`
    Worktree *ResourceState `json:"worktree,omitempty"`
    Tmux     *ResourceState `json:"tmux,omitempty"`
}

type Session struct {
    ID              string        `json:"id"`
    TmuxSessionName string        `json:"tmux_session_name,omitempty"`
    Name            string        `json:"name,omitempty"`
    WorkspaceID     string        `json:"workspace_id"`
    Branch          string        `json:"branch,omitempty"`
    BaseBranch      string        `json:"base_branch,omitempty"`
    BranchCreated   bool          `json:"branch_created,omitempty"`
    WorktreePath    string        `json:"worktree_path,omitempty"`
    Status          SessionStatus `json:"status"`
    Resources       *Resources    `json:"resources,omitempty"`
    IsAttached      bool          `json:"is_attached"`
    LastUsedAt      time.Time     `json:"last_used_at"`
}
```

### Resource lifecycle

**During creation** (async goroutine):
- Branch: `pending` → `creating` (git pull) → `ready`
- Worktree: `pending` → `creating` (git worktree add) → `ready`
- Tmux: `pending` → `creating` → `ready`
- Any failure: resource goes to `failed` with error, session Status=Broken

**During deletion** (synchronous, same as today):
- Worktree: `ready` → `removed`
- Branch: `ready` → `removed` (if deleteBranch=true)
- Tmux: `ready` → `removed`
- Session Status=Deleted

**During repair** (unified operation — single `RepairSession` method, replaces both `RetrySession` and `ReviveSession`):
- Find resources that aren't `ready`, recreate them
- Already-`ready` resources are skipped
- Session goes Creating → Ready (or Broken again if repair fails)

**Tmux killed externally** (reconciliation / hooks):
- Set Tmux resource status to Removed, session Status=Broken

Resources is always populated and persisted on the session. It serves as the persistent truth about what resources exist for a session, enabling future async refresh/reconciliation of resource state.

### Migration

`sessionstore.go` `OnAppStart` backfills existing sessions with no `Status`:
- `IsDeleted=true` → `StatusDeleted`
- `IsDead=true` → `StatusBroken` (with Tmux resource = Removed)
- default → `StatusReady`
- Copy `BranchName` → `Branch` if Branch is empty
- Build Resources from existing fields (WorktreePath, Branch, TmuxSessionName)
- Drop old fields from JSON on next save

### Error sentinels

Replace `ErrSessionNotDead`, `ErrSessionDead` with:
- `ErrSessionNotBroken` (for repair on non-broken session)
- `ErrCannotActivate` (for activating creating/broken session)

## Service Layer Changes

### `internal/session/sessionservice.go`

**`CreateSession` becomes two parts:**

1. **Synchronous** (HTTP request): validate, compute ID, build initial Resources with pending states, store session with Status=Creating, return immediately.

2. **Async** (goroutine via `runSetup`): execute steps sequentially, updating resource status before/after each. On success → Status=Ready. On failure → Status=Broken with failed resource error.

```
CreateSession(ctx, session, createWorktree):
  1. Validate workspace, compute ID (same as today)
  2. Build Resources based on what's needed:
     - If git repo + createWorktree: Branch={Pending}, Worktree={Pending}, Tmux={Pending}
     - Otherwise: Tmux={Pending}
  3. Set Status=Creating, LastUsedAt=now
  4. store.Add(session)
  5. Touch workspace
  6. go runSetup(session.ID, ws, createWorktree)
  7. Return session immediately

runSetup(sessionID, ws, createWorktree):
  Only acts on resources that are not Ready (already filtered by RefreshSession
  during repair, or set to Pending during initial creation).

  1. Load session from store
  2. If Branch resource exists and not Ready:
     a. Branch → Creating, store.Update
     b. git pull
     c. Branch → Ready, store.Update
  3. If Worktree resource exists and not Ready:
     a. Worktree → Creating, store.Update
     b. git worktree add (sets session.WorktreePath, session.Branch)
     c. Worktree → Ready, store.Update
  4. If Tmux resource not Ready:
     a. Tmux → Creating, store.Update
     b. tmuxManager.CreateSession
     c. Tmux → Ready, store.Update
  5. Status=Ready, store.Update

  On any error:
     - Set resource to Failed with error message
     - Set session Status=Broken
     - store.Update, return
```

**`RefreshSession(ctx, id)` — standalone method, checks actual state of each resource:**
```
RefreshSession(ctx, id):
  1. Load session from store
  2. For each resource in session.Resources:
     - Branch: skip refresh (no cheap way to verify pull status; best-effort)
     - Worktree: os.Stat(session.WorktreePath) — exists? Ready : Removed
     - Tmux: tmuxManager.HasSession(name) — exists? Ready : Removed
  3. Derive session Status from resource states:
     - All applicable resources Ready → StatusReady
     - Any resource Failed/Removed/Pending → StatusBroken
  4. store.Update, return session
```
This is independently callable — can be invoked by an API endpoint, reconciliation, or future async refresh without triggering repair.

**`RepairSession` (replaces both `RetrySession` and `ReviveSession`):**
```
RepairSession(ctx, id):
  1. Call RefreshSession(ctx, id) to get up-to-date resource state
  2. Verify Status=Broken (after refresh). If already Ready, return early.
  3. Set Status=Creating, clear errors on non-ready resources
  4. store.Update
  5. go runSetup(session.ID, ...) — only acts on non-ready resources
  6. Return session
```

**`BranchName` consolidation in CreateSession:**
Currently does `branchName := s.branchPrefix + session.Name` then sets both `BranchName` and `Branch`. Change to just set `Branch` directly.

**Update existing methods for new status model:**

- `ActivateSession`: reject if Status is Creating or Broken. If Broken, return error telling user to repair first.
- Remove `ReviveSession` — replaced by `RepairSession`.
- `DeleteSession`: reject if Status=Creating. Update Resources to track what was removed. Set Status=Deleted.
- `reconcileTmuxState`: call `RefreshSession` for each non-deleted session to reconcile actual state.
- Event handlers: update Status instead of boolean flags.
  - `handleTmuxSessionCreated`: call `RefreshSession` to update resource state
  - `handleTmuxSessionClosed`: call `RefreshSession` to update resource state
  - Attached/detached handlers: update IsAttached only (unchanged logic)

**`resolveWorkspaceName`**: keep as-is — it already resolves on read. Just remove `WorkspaceName` from the stored struct. Add it as a response-only field in `SessionResponse`.

## API Changes

### `internal/session/sessioncontroller.go` + `sessionrouter.go`

**New endpoint**: `PUT /sessions/{id}/repair` → `RepairSession`
- Returns 200 with session (status=creating)
- Returns 400 if session is not in broken state

**Remove endpoint**: `PUT /sessions/{name}/revive` — replaced by repair

**Existing endpoints:**
- `POST /sessions`: returns 202 Accepted with session in `creating` status
- `GET /sessions/{id}`: now includes `status` and `resources` fields

### `internal/session/types.go`

Remove `ReviveResult`. `SessionResponse` adds `WorkspaceName` as a response-only field (resolved by controller, not persisted):
```go
type SessionResponse struct {
    *Session
    WorkspaceName string `json:"workspace_name,omitempty"`
    WorkspacePath string `json:"workspace_path,omitempty"`
}
```

## TUI Changes

### Post-creation flow (`internal/tui/provider/sessionsprovider.go`)

**Current**: create → activate → switch tmux → quit
**New**: create → navigate to session list → poll until ready → auto-activate → switch → quit

Add `pendingSessionID` field to `sessionsProvider`. When set, each `sessionsLoadedMsg` checks the pending session's status:
- Ready → emit `activateSessionMsg` → switch tmux → quit
- Broken → emit `ErrMsg` with the error, clear pendingSessionID
- Creating → continue polling (already happening via status view 100ms ticks)

### Session list display (`internal/tui/sessionlist/sessionitem.go`)

Update `Title()`:
- `StatusCreating` → `"(creating...)"`
- `StatusBroken` → `"(broken)"`

### Session list keys (`internal/tui/sessionlist/sessionlist.go`)

Update `rebuildItems`: skip `StatusDeleted` instead of `IsDeleted`. Show broken sessions based on `showDead` flag (rename to `showBroken`).

Update select key handler:
- `StatusCreating` → status message "session is still being created"
- `StatusBroken` → show error from `Resources` as status message (e.g. "broken: timeout pulling branch main"). User must press Enter again to repair — this gives them a chance to fix the underlying issue first.
- `StatusReady` → activate (unchanged)

Track `pendingRepairID` — when a broken session is selected once, show the error. When selected a second time (same session), trigger `provider.RepairSession(id)` (similar to the double-press-to-delete pattern already used for `pendingDeleteID`).

### Client (`internal/tui/provider/client.go`)

Add `repairSession(id)` method: `PUT /sessions/{id}/repair`.
Remove `reviveSession(id)` method.

### Provider (`internal/tui/provider/sessionsprovider.go`)

Replace `ReviveSession` command with `RepairSession`.
Remove `reviveSessionIntentMsg` and `sessionRevivedMsg`, add `repairSessionIntentMsg`.

## Files to Modify

| File | Changes |
|------|---------|
| `internal/session/session.go` | New types, remove old booleans/BranchName/WorkspaceName/Cleanup, update errors |
| `internal/session/sessionservice.go` | Async CreateSession, runSetup, RepairSession, remove ReviveSession, update all status refs |
| `internal/session/sessioncontroller.go` | Repair endpoint, remove revive endpoint, resolve WorkspaceName in responses, 202 status |
| `internal/session/sessionrouter.go` | Add repair route, remove revive route |
| `internal/session/sessionstore.go` | Backfill migration in OnAppStart |
| `internal/session/types.go` | WorkspaceName in SessionResponse, remove ReviveResult |
| `internal/tui/provider/sessionsprovider.go` | pendingSessionID polling, RepairSession cmd, remove ReviveSession |
| `internal/tui/provider/client.go` | repairSession method, remove reviveSession |
| `internal/tui/sessionlist/sessionlist.go` | Update filters and key handlers, showBroken, pendingRepairID |
| `internal/tui/sessionlist/sessionitem.go` | Update Title() for new statuses |

## Implementation Order

1. **Session model** — new types, remove old fields, migration logic
2. **Session service** — async creation, runSetup, RepairSession, remove ReviveSession, update all methods
3. **Session controller + router** — repair endpoint, remove revive, response updates
4. **TUI provider** — polling, repair command, client method
5. **TUI session list** — display, key handlers

## Verification

1. `task build` — compiles cleanly
2. `task test` — tests pass (update tests for new model)
3. Manual: create session → appears as "creating..." → transitions to ready → auto-switches
4. Manual: simulate failure (bad branch) → shows "broken" → repair succeeds
5. Manual: kill tmux session externally → shows "broken" → repair recreates tmux
6. Manual: restart daemon with old sessions.json → migration works
