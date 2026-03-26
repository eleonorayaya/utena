# GitHub PR Integration — Technical Specification

## 1. Overview

This specification describes a restructuring of utena's data model and session lifecycle to support GitHub PR integration, lightweight session states, and proper git domain modeling.

### Goals

- Extract git state from session string fields into proper domain models (Repo, Branch, Worktree, PullRequest)
- Rework session state machine to support pending/inactive states without requiring all resources to be manifested
- Integrate with GitHub to sync PRs, auto-create pending sessions for assigned PRs, and expose structured diffs
- Introduce a generic background sync system for periodic tasks
- Define a unified signal/badge system for aggregating session attention indicators

### Non-Goals (this phase)

- TUI changes (will be a follow-up)
- PR creation, review, or approval from utena
- PR check status, review decisions, additions/deletions/changed files tracking
- Syntax-highlighted diff rendering

---

## 2. Git Module

### 2.1 Overview

`internal/git/` expands from a single stateless CLI wrapper (`gitservice.go`) into a full module with four domain models. The module exposes a **single `GitService`** to outside consumers. All internal details — the git CLI wrapper, GORM stores, and GitHub API client — are opaque to external modules.

The existing `gitservice.go` is renamed to `gitcli.go` and remains as the low-level git command wrapper. Its functionality is preserved but reorganized. A new `gitservice.go` serves as the public facade.

### 2.2 Models

#### Repo

```go
// internal/git/repo.go
type Repo struct {
    gorm.Model
    Path          string `gorm:"uniqueIndex"` // local clone path
    RemoteURL     string                       // origin remote URL
    FullName      string `gorm:"index"`        // "owner/repo"
    DefaultBranch string                       // typically "main"
}
```

#### Branch

```go
// internal/git/branch.go
type Branch struct {
    gorm.Model
    Name         string `gorm:"uniqueIndex:idx_branch_repo"`
    RepoID       uint   `gorm:"uniqueIndex:idx_branch_repo"`
    BaseBranchID *uint  // nullable FK to Branch
    ExistsLocal  bool
    ExistsRemote bool
    IsDirty      bool

    Repo       *Repo    // relationship
    BaseBranch *Branch  // self-referential relationship
    Worktree   *Worktree // has-one relationship (loaded via Preload)
}
```

`BaseBranchID` is a FK to another Branch record (e.g., the `main` branch record for a feature branch).

#### Worktree

```go
// internal/git/worktree.go
type Worktree struct {
    gorm.Model
    Path     string `gorm:"uniqueIndex"`
    BranchID uint   `gorm:"uniqueIndex"` // git enforces 1 worktree per branch
    RepoID   uint   `gorm:"index"`

    Branch *Branch // relationship
    Repo   *Repo   // relationship
}
```

A Worktree record exists if and only if the worktree physically exists on disk. Creating a worktree inserts a record; removing a worktree deletes the record.

#### PullRequest

```go
// internal/git/pullrequest.go
type PRState string

const (
    PRStateOpen   PRState = "open"
    PRStateClosed PRState = "closed"
    PRStateMerged PRState = "merged"
    PRStateDraft  PRState = "draft"
)

type PullRequest struct {
    gorm.Model
    Number       int       `gorm:"uniqueIndex:idx_pr_repo"`
    RepoID       uint      `gorm:"uniqueIndex:idx_pr_repo"`
    Title        string
    Body         string
    State        PRState
    URL          string
    HeadBranchID uint      `gorm:"index"`
    BaseBranchID *uint
    AuthorLogin  string
    IsDraft      bool
    MergedAt     *time.Time
    LastSyncedAt time.Time

    Repo       *Repo   // relationship
    HeadBranch *Branch // relationship
    BaseBranch *Branch // relationship
}
```

### 2.3 Structured Diff

```go
// internal/git/diff.go
type Diff struct {
    Files []DiffFile `json:"files"`
}

type FileStatus string

const (
    FileAdded    FileStatus = "added"
    FileModified FileStatus = "modified"
    FileDeleted  FileStatus = "deleted"
    FileRenamed  FileStatus = "renamed"
)

type DiffFile struct {
    Path    string     `json:"path"`
    OldPath string     `json:"old_path,omitempty"`
    Status  FileStatus `json:"status"`
    Hunks   []DiffHunk `json:"hunks"`
}

type DiffHunk struct {
    OldStart int        `json:"old_start"`
    OldLines int        `json:"old_lines"`
    NewStart int        `json:"new_start"`
    NewLines int        `json:"new_lines"`
    Header   string     `json:"header"`
    Lines    []DiffLine `json:"lines"`
}

type DiffLineType string

const (
    DiffLineContext DiffLineType = "context"
    DiffLineAdd     DiffLineType = "add"
    DiffLineDelete  DiffLineType = "delete"
)

type DiffLine struct {
    Type    DiffLineType `json:"type"`
    Content string       `json:"content"`
    OldNum  *int         `json:"old_num,omitempty"`
    NewNum  *int         `json:"new_num,omitempty"`
}
```

A diff parser converts GitHub's unified diff format into this structured representation.

### 2.4 Module Structure

```
internal/git/
    repo.go              # Repo model
    branch.go            # Branch model
    worktree.go          # Worktree model
    pullrequest.go       # PullRequest model
    diff.go              # Structured diff types
    diffparser.go        # Unified diff → structured diff parser
    repostore.go         # GORM store for Repo
    branchstore.go       # GORM store for Branch
    worktreestore.go     # GORM store for Worktree
    prstore.go           # GORM store for PullRequest
    gitcli.go            # Low-level git CLI wrapper (reworked from old gitservice.go)
    githubclient.go      # GitHub GraphQL + REST client
    gitservice.go        # Single public service (facade)
    gitmodule.go         # Module wiring
```

The module has **no controller or router**. All git operations are triggered internally by background sync tasks or as side effects of other module APIs (e.g., session activation calls `gitService.EnsureWorktree`).

### 2.5 GitService — Public Facade

```go
type GitService struct {
    cli           *gitCLI
    githubClient  GitHubClient
    repoStore     *RepoStore
    branchStore   *BranchStore
    worktreeStore *WorktreeStore
    prStore       *PRStore
    branchPrefix  string
    bus           eventbus.EventBus
}
```

#### Repo Operations

| Method | Description |
|--------|-------------|
| `FindOrCreateRepo(ctx, localPath string) (*Repo, error)` | Discovers remote URL, parses owner/repo, upserts Repo record |
| `GetRepo(ctx, id uint) (*Repo, error)` | Load by ID |
| `GetRepoByPath(ctx, path string) (*Repo, error)` | Load by local path |

#### Branch Operations

| Method | Description |
|--------|-------------|
| `FindOrCreateBranch(ctx, repoID uint, name string, baseBranchID *uint) (*Branch, error)` | Upsert branch record |
| `CreateBranch(ctx, repoID uint, name, baseBranchName string) (*Branch, error)` | Creates new git branch + worktree, returns Branch with Worktree |
| `GetBranch(ctx, branchID uint) (*Branch, error)` | Load with Worktree preloaded |
| `ListBranches(ctx, repoID uint) ([]Branch, error)` | List all branches for repo |
| `EnsureWorktree(ctx, branchID uint) (string, error)` | Pulls if needed, creates worktree if missing, returns worktree path |
| `GetStartDir(ctx, branchID uint) (string, error)` | Returns worktree path if exists, else repo path |
| `SyncBranch(ctx, branchID uint) error` | Updates ExistsLocal, ExistsRemote, IsDirty; reconciles Worktree record |
| `CleanupBranch(ctx, branchID uint, deleteBranch bool) error` | Removes worktree, optionally deletes branch |
| `HasWorktree(ctx, branchID uint) bool` | Checks if Worktree record exists |
| `IsHealthy(ctx, branchID uint) bool` | Validates worktree path matches expected branch |

#### PR Operations

| Method | Description |
|--------|-------------|
| `GetPR(ctx, repoID uint, number int) (*PullRequest, error)` | Fetches single PR from GitHub API, upserts into store |
| `SearchPRs(ctx, filter PRFilter) ([]PullRequest, error)` | Search PRs with filters (see below) |
| `GetPRDiff(ctx, prID uint) (*Diff, error)` | Fetches diff from GitHub REST API, returns structured diff |
| `GetPRsForBranch(ctx, branchID uint) ([]PullRequest, error)` | Load from store by HeadBranchID |

#### Sync Operations (called by background tasks)

| Method | Description |
|--------|-------------|
| `SyncRepoPRs(ctx, repoID uint) error` | Fetches open PRs for repo, upserts, detects state changes, publishes events |
| `SyncAssignedPRs(ctx) ([]PullRequest, error)` | Fetches PRs assigned to authenticated user, returns newly discovered PRs |
| `SyncBranches(ctx, repoID uint) error` | Syncs state of all tracked branches for repo |

#### PR Search Filters

```go
type PRFilter struct {
    RepoID      *uint
    BranchID    *uint
    State       *PRState
    AuthorLogin *string
    AssignedTo  *string  // "@me" or username
    Search      *string  // free text search
}
```

Filters are applied at the store level where possible (SQL WHERE clauses) and at the GitHub API level for search/assigned queries.

### 2.6 GitHub Client

```go
// internal/git/githubclient.go
type GitHubClient interface {
    ListRepoPRs(ctx context.Context, owner, repo string) ([]RawPR, error)
    ListAssignedPRs(ctx context.Context) ([]RawPR, error)
    GetPR(ctx context.Context, owner, repo string, number int) (*RawPR, error)
    GetPRDiff(ctx context.Context, owner, repo string, number int) (string, error)
    GetCurrentUser(ctx context.Context) (string, error)
}
```

Implementation uses `shurcooL/githubv4` for GraphQL queries and `net/http` for the REST diff endpoint (GitHub GraphQL does not serve diffs).

Auth token resolution (in order):
1. `GITHUB_TOKEN` environment variable
2. Output of `gh auth token` (if `gh` CLI is installed)
3. If neither is available, the module starts in **degraded mode**: PR features are disabled, branch/worktree features continue to work. A warning is logged at startup.

### 2.7 EventBus Events

| Event | Data | Published When |
|-------|------|----------------|
| `git.pr_state_changed` | `PRStateChangedEvent` | PR transitions state (open→merged, etc.) |
| `git.pr_discovered` | `PRDiscoveredEvent` | New PR found during sync |
| `git.branch_updated` | `BranchUpdatedEvent` | Branch state changed after sync |

```go
type PRStateChangedEvent struct {
    PRID         uint
    PRNumber     int
    RepoID       uint
    HeadBranchID uint
    OldState     PRState
    NewState     PRState
}

type PRDiscoveredEvent struct {
    PRID         uint
    PRNumber     int
    RepoID       uint
    HeadBranchID uint
    AuthorLogin  string
}

type BranchUpdatedEvent struct {
    BranchID uint
    RepoID   uint
}
```

### 2.8 gitCLI — Internal Wrapper

Renamed from the current `GitService`. Same operations, not exported beyond the git package:

```go
type gitCLI struct{}

func (c *gitCLI) listBranches(ctx, repoPath) ([]string, error)
func (c *gitCLI) pull(ctx, repoPath, branch) error
func (c *gitCLI) createWorktree(ctx, repoPath, branchName, baseBranch) (string, error)
func (c *gitCLI) checkoutWorktree(ctx, repoPath, branch) (string, error)
func (c *gitCLI) removeWorktree(ctx, repoPath, worktreePath) error
func (c *gitCLI) deleteBranch(ctx, repoPath, branchName) error
func (c *gitCLI) currentBranch(ctx, repoPath) (string, error)
func (c *gitCLI) isDirty(ctx, repoPath) (bool, error)
func (c *gitCLI) hasBranch(ctx, repoPath, branch) (bool, error)
func (c *gitCLI) hasRemoteBranch(ctx, repoPath, branch) (bool, error)
func (c *gitCLI) validateWorktree(ctx, worktreePath, expectedBranch) (bool, error)
func (c *gitCLI) worktreePath(repoPath, branch) string
func (c *gitCLI) remoteURL(ctx, repoPath) (string, error)
```

---

## 3. Tmux Module

### 3.1 TmuxSession Model

```go
// internal/tmux/tmuxsession.go
type TmuxSession struct {
    gorm.Model
    Name     string            `gorm:"uniqueIndex"` // tmux session name
    StartDir string                                  // directory the session was started in
    Env      map[string]string `gorm:"serializer:json"` // environment variables
    IsAlive  bool                                    // updated by hooks and sync
}
```

A record exists when utena has created (or intends to manage) a tmux session. `IsAlive` is updated by tmux hook events and the session reconciliation sync task.

### 3.2 Consolidated TmuxService

The current architecture has three layers: `TmuxClient` interface → `gotmuxClient` implementation → `TmuxService` wrapper. This is consolidated into a single `TmuxService` that handles both low-level tmux operations and persistence.

```go
// internal/tmux/tmuxservice.go
type TmuxService struct {
    tmux  *gotmux.Tmux     // gotmux client (direct, no interface wrapper)
    store *TmuxStore        // GORM store for TmuxSession records
    bus   eventbus.EventBus
}
```

The `TmuxClient` interface is removed. `TmuxService` calls `gotmux` directly and manages DB persistence in the same operations.

#### Methods

| Method | Description |
|--------|-------------|
| `CreateSession(name, startDir string, env map[string]string) (*TmuxSession, error)` | Creates tmux session + DB record. Returns the persisted TmuxSession |
| `KillSession(id uint) error` | Kills tmux session + deletes DB record |
| `RecreateSession(id uint) error` | Recreates a dead tmux session from stored config (name, startDir, env). Sets IsAlive=true |
| `HasSession(name string) bool` | Checks if tmux session exists (live check via gotmux) |
| `SwitchClient(targetSession string) error` | Switches tmux client to target session |
| `GetSession(id uint) (*TmuxSession, error)` | Load from store |
| `GetSessionByName(name string) (*TmuxSession, error)` | Load from store |
| `GetCurrentSessionName(paneID string) (string, error)` | Queries tmux for session name |
| `HandleSessionCreated(ctx, tmuxName) error` | Hook: sets IsAlive=true, publishes event |
| `HandleSessionClosed(ctx, tmuxName) error` | Hook: sets IsAlive=false, publishes event |
| `HandleClientSessionChanged(ctx, tmuxName) error` | Hook: publishes event |
| `HandleClientAttached(ctx, tmuxName) error` | Hook: publishes event |
| `HandleClientDetached(ctx, tmuxName) error` | Hook: publishes event |
| `SyncWindows(ctx, tmuxName, windows)` | Updates window state (existing functionality) |
| `GetWindows(ctx, tmuxName) []Window` | Returns window state (existing functionality) |
| `ListSessionNames() ([]string, error)` | Lists all live tmux session names |

For testing, the gotmux dependency can be injected as an interface at the `TmuxService` level if needed, but this is an internal concern — external modules only see `TmuxService`.

### 3.3 Module Structure

```
internal/tmux/
    tmuxsession.go     # TmuxSession model
    tmuxstore.go       # GORM store
    tmuxservice.go     # Consolidated service (gotmux + store + events)
    tmuxcontroller.go  # Hook HTTP endpoints (existing, unchanged API)
    tmuxrouter.go      # Routes (existing, unchanged)
    tmuxmodule.go      # Updated: accepts DB, registers TmuxSession model
    types.go           # Existing types (HookEvent, Window, etc.)
```

### 3.4 Module Changes

```go
type TmuxModule struct {
    Service    *TmuxService
    Controller *TmuxController
    Router     *TmuxRouter
}

func NewTmuxModule(bus eventbus.EventBus, database db.Database) *TmuxModule {
    tmux, _ := gotmux.DefaultTmux() // nil if tmux not available
    store := NewTmuxStore(database)
    service := NewTmuxService(tmux, store, bus)
    // ...
}

func (m *TmuxModule) Models() []any {
    return []any{&TmuxSession{}}
}
```

---

## 4. Workspace Module Changes

### 4.1 Model Change

```go
type Workspace struct {
    gorm.Model
    Name       string
    Path       string    `gorm:"uniqueIndex"`
    RepoID     *uint     `gorm:"index"` // nullable FK to git.Repo (replaces IsGitRepo bool)
    LastUsedAt time.Time

    Repo *git.Repo // relationship
}
```

`IsGitRepo` is removed. A workspace is a git repo if `RepoID != nil`.

### 4.2 Module Changes

- `NewWorkspaceModule` accepts `*git.GitModule` (not individual services)
- Module init extracts `gitModule.Service` and passes to `WorkspaceService`
- `WorkspaceService.CreateWorkspace` calls `gitService.FindOrCreateRepo(path)` for git-backed workspaces and sets `RepoID`
- The workspace module no longer creates or holds its own `GitService` instance
- Branch listing delegates to `gitService.ListBranches(repoID)`

---

## 5. Session State Machine

### 5.1 States

```go
const (
    StatusPending  SessionStatus = "pending"   // tracked, not materialized
    StatusCreating SessionStatus = "creating"  // setup in progress
    StatusActive   SessionStatus = "active"    // fully operational, tmux running
    StatusInactive SessionStatus = "inactive"  // was active, tmux not running, reactivatable
    StatusBroken   SessionStatus = "broken"    // needs manual intervention
    StatusArchived SessionStatus = "archived"  // work complete
    StatusDeleted  SessionStatus = "deleted"   // terminal
)
```

### 5.2 State Diagram

```
                 ┌──────────┐
                 │ pending   │ ← PR discovery / manual pre-creation
                 └─────┬─────┘
                       │ activate
                       ▼
                 ┌──────────┐         repair
       ┌────────►│ creating  │◄──────────────┐
       │         └─────┬─────┘               │
       │               │ success             │
       │               ▼                     │
       │         ┌──────────┐  tmux dies  ┌──┴───────┐
       │         │  active   ├───────────►│ inactive  │
       │         └──┬───┬───┘◄────────────┴──┬───────┘
       │            │   │     activate        │
       │            │   │                     │
       │            ▼   ▼                     ▼
       │      ┌──────────┐            ┌──────────┐
       │      │ archived  │            │  broken   │
       │      └─────┬─────┘            └──────────┘
       │            │
       │            ▼
       │      ┌──────────┐
       └──────│ deleted   │ ← from any non-terminal via force delete
              └──────────┘
```

### 5.3 Transition Table

| From | To | Trigger | Async |
|---|---|---|---|
| pending | creating | Activate | Yes |
| pending | deleted | Dismiss | No |
| creating | active | Setup completes | (background) |
| creating | inactive | Git ready, tmux failed | (background) |
| creating | broken | Unrecoverable git failure | (background) |
| active | inactive | Tmux session dies (hook) | (event) |
| inactive | creating | Activate | Yes |
| inactive | broken | Worktree/branch disappears (sync) | (background) |
| active | archived | User archives | No |
| inactive | archived | User archives | No |
| broken | creating | Repair | Yes |
| archived | deleted | User deletes | No |
| any non-terminal | deleted | Force delete | No |

### 5.4 Key Design Decisions

**All activation is async.** Every call to `ActivateSession` returns `202 Accepted` immediately and runs setup in a background goroutine. This is true for all source states:
- Pending → sets `creating`, launches full setup (branch + worktree + tmux)
- Inactive → sets `creating`, launches tmux recreation (+ validates git state)
- Active → sets `creating`, launches tmux client switch (fast, but still async for consistency)

The TUI polls or receives updates to reflect the final state.

**Tmux restart → inactive, not broken.** When `tmux.session_closed` fires, the session transitions to `inactive`. The branch and worktree still exist. `TmuxSession.IsAlive` is set to false. Reactivation recreates the tmux session.

**Broken = manual intervention needed.** Reserved for genuinely inconsistent states: worktree path exists but has the wrong branch checked out, branch was deleted externally while worktree exists, unresolvable git conflicts. Not for normal tmux lifecycle events.

### 5.5 Session Model

```go
type Session struct {
    gorm.Model
    Name          string                       `json:"name"`
    WorkspaceID   uint                         `json:"workspace_id" gorm:"index"`
    TodoID        *uint                        `json:"todo_id,omitempty" gorm:"index"`
    BranchID      *uint                        `json:"branch_id,omitempty" gorm:"index"`
    TmuxSessionID *uint                        `json:"tmux_session_id,omitempty" gorm:"index"`
    Status        SessionStatus                `json:"status"`
    IsAttached    bool                         `json:"is_attached"`
    LastUsedAt    time.Time                    `json:"last_used_at"`

    GitBranch      *git.Branch              `json:"git_branch,omitempty" gorm:"foreignKey:BranchID"`
    TmuxSession    *tmux.TmuxSession        `json:"tmux_session,omitempty" gorm:"foreignKey:TmuxSessionID"`
    Workspace      *workspace.Workspace     `json:"workspace,omitempty" gorm:"foreignKey:WorkspaceID"`
    ClaudeSessions []claude.ClaudeSession   `json:"claude_sessions,omitempty" gorm:"foreignKey:SessionID"`
}
```

**Removed from current model:** `TmuxSessionName`, `Branch`, `BaseBranch`, `WorktreePath`, `Resources`

**Added:** `BranchID`, `TmuxSessionID`

State is derived from associated models rather than a `Resources` JSON blob:
- Git ready = `BranchID` is set and `gitService.HasWorktree(branchID)` is true
- Tmux ready = `TmuxSessionID` is set and `tmuxSession.IsAlive` is true

### 5.6 PRs Through Branch Association

Sessions have **no direct link** to PRs. PRs are loaded through the branch:

```
Session → BranchID → Branch ← HeadBranchID ← PullRequest[]
```

The session controller calls `gitService.GetPRsForBranch(session.BranchID)` when building API responses. This keeps the session module git-agnostic beyond holding a `BranchID`.

### 5.7 Dismissed PRs

```go
// internal/session/dismissedpr.go
type DismissedPR struct {
    gorm.Model
    PullRequestID uint      `gorm:"uniqueIndex"`
    DismissedAt   time.Time
}
```

When the session module receives a `git.pr_discovered` event, it checks this table before auto-creating a pending session.

### 5.8 Activation Flow (async)

```
ActivateSession(ctx, id uint) → (202 Accepted)
    1. Load session + associations
    2. Validate status is activatable (pending, inactive, active)
    3. Set status = creating
    4. Persist
    5. Launch goroutine: runActivation(session.ID)
    6. Return 202

runActivation(sessionID):
    Load session
    Switch on previous status:

    was pending:
        a. Generate tmux session name (workspace + session name)
        b. gitService.EnsureWorktree(branchID) → startDir
        c. Run worktree init scripts if worktree was newly created
        d. tmuxService.CreateSession(name, startDir, env) → tmuxSession
        e. Set session.TmuxSessionID = tmuxSession.ID
        f. Set status = active

    was inactive:
        a. Validate git state (gitService.IsHealthy(branchID))
        b. If unhealthy → status = broken, return
        c. tmuxService.RecreateSession(tmuxSessionID)
        d. Set status = active

    was active:
        a. tmuxService.SwitchClient(tmuxSession.Name)
        b. Status stays active

    Set IsAttached = true, LastUsedAt = now
    Persist

    On any error:
        If git-related → status = broken
        If tmux-related → status = inactive
        Persist error details (TBD: error field on session or log)
```

### 5.9 Session API

**Existing endpoints (updated):**

```
GET    /sessions                         # list sessions (includes signals, branch, PRs)
POST   /sessions                         # create session
GET    /sessions/{id}                    # get session detail
PUT    /sessions/{id}                    # update session
DELETE /sessions/{id}                    # delete session
PUT    /sessions/{id}/activate           # activate (always async, 202)
PUT    /sessions/{id}/repair             # repair broken session (async, 202)
```

**New endpoints:**

```
PUT    /sessions/{id}/archive            # archive a session
PUT    /sessions/{id}/dismiss            # dismiss a pending session
```

### 5.10 Session API Response Shape

```json
{
    "id": 1,
    "name": "feature-login",
    "status": "active",
    "workspace_id": 5,
    "is_attached": true,
    "last_used_at": "2026-03-26T10:00:00Z",
    "signals": [
        {"source": "pr", "key": "pr:42", "severity": "info", "label": "PR #42 open"},
        {"source": "claude", "key": "claude:5", "severity": "active", "label": "working"}
    ],
    "top_severity": "active",
    "branch": {
        "id": 12,
        "name": "eqt/feature-login",
        "exists_local": true,
        "exists_remote": true,
        "is_dirty": false,
        "has_worktree": true
    },
    "pull_requests": [
        {"id": 7, "number": 42, "title": "Add login feature", "state": "open", "url": "..."}
    ],
    "tmux_session": {
        "id": 3,
        "name": "utena-feature-login",
        "is_alive": true
    },
    "claude_sessions": [...]
}
```

---

## 6. Background Sync System

### 6.1 Design

A generic `SyncManager` in `internal/sync/`. Modules register periodic tasks. The sync system has no REST API — all management is internal.

```go
// internal/sync/sync.go
type SyncTask interface {
    Name() string
    Interval() time.Duration
    Run(ctx context.Context) error
}

type SyncManager struct {
    tasks   []SyncTask
    stopChs map[string]chan struct{}
    mu      sync.Mutex
}

func NewSyncManager() *SyncManager
func (m *SyncManager) Register(task SyncTask)
func (m *SyncManager) Start(ctx context.Context)
func (m *SyncManager) Stop()
func (m *SyncManager) TriggerSync(ctx context.Context, taskName string) error
```

Each registered task runs in its own goroutine on a `time.Ticker`. Errors are logged but do not stop the loop. `TriggerSync` allows manual triggering (e.g., from other services that want an immediate refresh).

### 6.2 Registered Tasks

| Task | Module | Interval | Description |
|------|--------|----------|-------------|
| `git.prs` | Git | 60s | Sync PRs for all repos. Upsert, detect state changes, publish events. Sync assigned PRs. |
| `git.branches` | Git | 120s | Sync branch state (local/remote/dirty) for branches with active sessions |
| `session.reconcile` | Session | 30s | Check tmux state for all non-terminal sessions. Transition active↔inactive as needed. Detect broken states. |

### 6.3 Wiring

Modules expose a `RegisterSyncTasks(manager *sync.SyncManager)` method. Called during app initialization after all modules are created but before `Start`.

```go
// app.go
syncManager := sync.NewSyncManager()
gitModule.RegisterSyncTasks(syncManager)
sessionModule.RegisterSyncTasks(syncManager)
// syncManager.Start(ctx) called in app.OnStart after all modules started
```

---

## 7. Signal System

### 7.1 Design

Signals are computed per-model, not per-session. Each domain model knows how to produce its own signals via a `Signals()` method. The session controller aggregates signals from all associated models when building API responses.

No module depends on the session module. The dependency direction is: session controller → loads associated models → calls their `Signals()` methods.

### 7.2 Signal Type

```go
// internal/common/signals.go
type SignalSeverity string

const (
    SeverityInfo    SignalSeverity = "info"     // informational
    SeverityActive  SignalSeverity = "active"   // something is happening
    SeveritySuccess SignalSeverity = "success"  // positive outcome
    SeverityWarning SignalSeverity = "warning"  // needs attention soon
    SeverityUrgent  SignalSeverity = "urgent"   // needs attention now
)

type Signal struct {
    Source   string         `json:"source"`   // "pr", "claude", "git", "session", "tmux"
    Key      string         `json:"key"`      // unique id: "pr:42", "claude:5"
    Severity SignalSeverity `json:"severity"`
    Label    string         `json:"label"`    // display text
}

func TopSeverity(signals []Signal) SignalSeverity
```

### 7.3 Signal Methods on Models

```go
// git module
func (pr *PullRequest) Signals() []common.Signal
// merged → info "PR #N merged"
// draft → info "PR #N draft"
// open → info "PR #N open"

func (b *Branch) Signals() []common.Signal
// dirty → warning "uncommitted changes"
// !ExistsLocal && ExistsRemote → info "remote only"

// tmux module
func (ts *TmuxSession) Signals() []common.Signal
// !IsAlive → info "tmux stopped"

// claude module
func (cs *ClaudeSession) Signals() []common.Signal
// NeedsAttention → urgent
// Working → active
// ReadyForReview → warning
// Done → success
// Idle → info
```

### 7.4 Aggregation in Session Controller

When building a session response:
1. Load session with `GitBranch` and `TmuxSession` via joins
2. Preload `ClaudeSessions`
3. Call `gitService.GetPRsForBranch(branchID)` to get PRs
4. Collect signals from: `branch.Signals()`, each `pr.Signals()`, `tmuxSession.Signals()`, each `claudeSession.Signals()`
5. Add session-level signals (e.g., pending → info, broken → urgent, archived → info)
6. Compute `TopSeverity`
7. Include all in response

---

## 8. Pending Session Creation

### 8.1 Flow

During the `git.prs` sync task:
1. `gitService.SyncAssignedPRs(ctx)` fetches PRs assigned to the authenticated user
2. For newly discovered PRs, the git module publishes `git.pr_discovered` events

The session module subscribes to `git.pr_discovered`:
1. Find workspace where `workspace.RepoID == event.RepoID`
2. If no matching workspace → skip
3. Check if any existing session has `BranchID == event.HeadBranchID` → skip
4. Check `DismissedPR` table for `event.PRID` → skip if dismissed
5. Create pending session:
   - `Status = StatusPending`
   - `BranchID = event.HeadBranchID`
   - `WorkspaceID = workspace.ID`
   - `Name = branchNameToSessionName(branch.Name)`
   - `TmuxSessionID = nil`

When user activates the pending session, the async activation flow handles full setup.

---

## 9. Module Dependency Graph

```
                    ┌──────────┐
                    │    DB    │
                    └────┬─────┘
                         │
          ┌──────────────┼──────────────┐
          │              │              │
     ┌────▼────┐   ┌─────▼─────┐  ┌────▼────┐
     │   Git   │   │   Tmux    │  │  Claude  │
     └────┬────┘   └─────┬─────┘  └────┬────┘
          │              │              │
     ┌────▼────┐         │              │
     │Workspace│         │              │
     └────┬────┘         │              │
          │              │              │
          └──────┬───────┘──────────────┘
                 │
           ┌─────▼─────┐
           │  Session   │
           └─────┬──────┘
                 │
           ┌─────▼─────┐
           │   Sync     │
           └────────────┘
```

**Module init pattern:**
```go
gitModule := git.NewGitModule(database, bus, cfg)
tmuxModule := tmux.NewTmuxModule(bus, database)
claudeModule := claude.NewClaudeModule(database)
workspaceModule := workspace.NewWorkspaceModule(database, fs, gitModule, configDir)
sessionModule := session.NewSessionModule(gitModule, tmuxModule, workspaceModule, claudeModule, bus, database, cfg)

syncManager := sync.NewSyncManager()
gitModule.RegisterSyncTasks(syncManager)
sessionModule.RegisterSyncTasks(syncManager)
```

---

## 10. Configuration

| Config | Default | Description |
|--------|---------|-------------|
| `GitHubSyncInterval` | 60s | How often to sync PRs from GitHub |
| `BranchSyncInterval` | 120s | How often to sync branch state |
| `SessionReconcileInterval` | 30s | How often to reconcile session/tmux state |
| `BranchPrefix` | `eqt/` | Prefix for new branches |
| `GitHubEnabled` | true | Auto-disabled if no token found |

---

## 11. Edge Cases

| Scenario | Behavior |
|---|---|
| Branch with no PR | Session works as before, no PR signals |
| PR with no local branch | Pending session created. Activation creates worktree + tmux |
| Multiple PRs for same branch | Multiple PullRequest records with same HeadBranchID. All returned by `GetPRsForBranch` |
| PR merged, worktree exists | Signal: "PR #N merged". User prompted to archive via TUI. No auto-cleanup |
| Non-git workspace | `RepoID` is nil. No git/PR features visible |
| No GitHub token | Git module degraded mode. Branch/worktree work. PR sync disabled |
| Tmux restart | Active sessions → inactive. `TmuxSession.IsAlive = false`. Activation recreates tmux |
| Worktree deleted externally | Branch sync detects → deletes Worktree record. Session reconcile → broken |
| Dismissed PR | DismissedPR record prevents pending session re-creation |
| Stacked PRs (multiple PRs, same branch) | All loaded via branch association. Session shows all. Archives when all merged/closed |
