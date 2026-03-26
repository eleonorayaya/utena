# GitHub PR Integration — Task Breakdown

## Dependency Legend

- `→` means "blocks" (right side cannot start until left completes)
- Tasks at the same level with no dependency arrows can be done in parallel
- Each task includes its own test coverage

---

## Phase 1: Foundation Models (parallelizable)

These tasks create the new domain models and stores. They have no dependencies on each other and can all be done in parallel.

### Task 1.1: Signal types

Create the shared signal types used by all modules.

**Files:**
- `internal/common/signals.go`
- `internal/common/signals_test.go`

**Work:**
- Define `SignalSeverity` constants: `info`, `active`, `success`, `warning`, `urgent`
- Define `Signal` struct: `Source`, `Key`, `Severity`, `Label`
- Implement `TopSeverity(signals []Signal) SignalSeverity` helper
- Implement `SortSignals(signals []Signal)` to sort by severity descending

**Tests:**
- `TopSeverity` returns highest severity from mixed list
- `TopSeverity` returns empty for empty list
- `SortSignals` orders by severity descending

**Dependencies:** None

---

### Task 1.2: Repo model + store

**Files:**
- `internal/git/repo.go`
- `internal/git/repostore.go`
- `internal/git/repostore_test.go`

**Work:**
- Define `Repo` struct with GORM tags (Path uniqueIndex, FullName index)
- Implement `RepoStore`: `Add`, `GetByID`, `GetByPath`, `GetByFullName`, `List`, `Update`, `Upsert`
- `Upsert` finds by Path, creates if not found, updates if found

**Tests (in-memory SQLite):**
- Add repo and retrieve by ID
- GetByPath returns correct repo
- GetByFullName returns correct repo
- Upsert creates when not found
- Upsert updates when found
- Path uniqueness constraint enforced
- List returns all repos

**Dependencies:** None

---

### Task 1.3: Branch model + store

**Files:**
- `internal/git/branch.go`
- `internal/git/branchstore.go`
- `internal/git/branchstore_test.go`

**Work:**
- Define `Branch` struct with GORM tags (unique composite index on Name+RepoID, self-referential BaseBranchID FK)
- Implement `BranchStore`: `Add`, `GetByID`, `GetByNameAndRepo`, `ListByRepo`, `Update`, `Upsert`, `Delete`
- `Upsert` finds by Name+RepoID, creates or updates
- `GetByID` preloads Worktree relationship
- Add `Signals() []common.Signal` method on Branch model

**Tests (in-memory SQLite):**
- Add branch and retrieve by ID
- GetByNameAndRepo returns correct branch
- Unique constraint on (Name, RepoID)
- BaseBranchID self-reference loads correctly
- ListByRepo filters correctly
- Upsert creates/updates
- Signals: dirty branch returns warning signal
- Signals: remote-only branch returns info signal
- Signals: clean local branch returns no signals

**Dependencies:** Task 1.1 (for Signal types), Task 1.2 (for Repo FK)

---

### Task 1.4: Worktree model + store

**Files:**
- `internal/git/worktree.go`
- `internal/git/worktreestore.go`
- `internal/git/worktreestore_test.go`

**Work:**
- Define `Worktree` struct (Path uniqueIndex, BranchID uniqueIndex, RepoID index)
- Implement `WorktreeStore`: `Add`, `GetByID`, `GetByBranchID`, `GetByPath`, `Delete`, `DeleteByBranchID`, `ListByRepo`
- Record existence = worktree physically exists

**Tests (in-memory SQLite):**
- Add worktree and retrieve by branch ID
- Path uniqueness enforced
- BranchID uniqueness enforced (one worktree per branch)
- DeleteByBranchID removes record
- GetByBranchID returns nil when no worktree exists
- ListByRepo filters correctly

**Dependencies:** Task 1.2, Task 1.3

---

### Task 1.5: PullRequest model + store

**Files:**
- `internal/git/pullrequest.go`
- `internal/git/prstore.go`
- `internal/git/prstore_test.go`

**Work:**
- Define `PRState` constants: `open`, `closed`, `merged`, `draft`
- Define `PullRequest` struct with GORM tags (composite unique on RepoID+Number, HeadBranchID index)
- Implement `PRStore`: `Add`, `GetByID`, `GetByRepoAndNumber`, `ListByRepo`, `ListByBranch`, `ListByState`, `Upsert`, `Update`
- `ListByBranch` filters by `HeadBranchID`
- `Upsert` matches on RepoID+Number
- Add `Signals() []common.Signal` method on PullRequest model

**Tests (in-memory SQLite):**
- Add PR and retrieve by ID
- GetByRepoAndNumber returns correct PR
- Unique constraint on (RepoID, Number)
- ListByBranch returns PRs for given branch
- ListByState filters correctly
- Upsert creates/updates
- Signals: open PR returns info signal
- Signals: merged PR returns info signal with "merged" label
- Signals: draft PR returns info signal with "draft" label

**Dependencies:** Task 1.1, Task 1.2, Task 1.3

---

### Task 1.6: Diff types + parser

**Files:**
- `internal/git/diff.go`
- `internal/git/diffparser.go`
- `internal/git/diffparser_test.go`

**Work:**
- Define `Diff`, `DiffFile`, `DiffHunk`, `DiffLine`, `DiffLineType`, `FileStatus` types
- Implement `ParseUnifiedDiff(raw string) (*Diff, error)` — parses GitHub unified diff format
- Handle: added/modified/deleted/renamed files, multiple hunks, context/add/delete lines, line numbers

**Tests:**
- Parse single file with one hunk
- Parse multiple files
- Parse file with multiple hunks
- Parse renamed file
- Parse added file (no old path)
- Parse deleted file
- Handle empty diff
- Handle malformed input gracefully
- Line numbers computed correctly

**Dependencies:** None

---

### Task 1.7: TmuxSession model + store

**Files:**
- `internal/tmux/tmuxsession.go`
- `internal/tmux/tmuxstore.go`
- `internal/tmux/tmuxstore_test.go`

**Work:**
- Define `TmuxSession` struct (Name uniqueIndex, StartDir, Env serialized JSON, IsAlive)
- Implement `TmuxStore`: `Add`, `GetByID`, `GetByName`, `List`, `Update`, `Delete`
- Add `Signals() []common.Signal` method on TmuxSession model

**Tests (in-memory SQLite):**
- Add session and retrieve by ID
- GetByName returns correct session
- Name uniqueness enforced
- Update IsAlive flag
- Env JSON serialization round-trips correctly
- Delete removes record
- Signals: alive session returns no signals
- Signals: dead session returns info "tmux stopped" signal

**Dependencies:** Task 1.1

---

### Task 1.8: DismissedPR model + store

**Files:**
- `internal/session/dismissedpr.go`
- `internal/session/dismissedprstore.go`
- `internal/session/dismissedprstore_test.go`

**Work:**
- Define `DismissedPR` struct (PullRequestID uniqueIndex, DismissedAt)
- Implement `DismissedPRStore`: `Add`, `IsDismissed(prID uint) bool`, `Delete`

**Tests (in-memory SQLite):**
- Add dismissed PR and check IsDismissed returns true
- IsDismissed returns false for non-dismissed PR
- Uniqueness enforced
- Delete removes record, IsDismissed returns false

**Dependencies:** None

---

### Task 1.9: SyncManager

**Files:**
- `internal/sync/sync.go`
- `internal/sync/sync_test.go`

**Work:**
- Define `SyncTask` interface: `Name() string`, `Interval() time.Duration`, `Run(ctx context.Context) error`
- Implement `SyncManager`: `Register`, `Start`, `Stop`, `TriggerSync`
- Each task runs on its own goroutine with `time.Ticker`
- Errors logged, don't stop the loop
- `Stop` cancels all goroutines gracefully
- `TriggerSync` runs a named task immediately (non-blocking, queues if already running)

**Tests:**
- Register and start a task, verify it runs at interval
- Stop cancels running tasks
- TriggerSync runs task immediately
- Task errors don't crash the manager
- Multiple tasks run independently
- TriggerSync for unknown task returns error

**Dependencies:** None

---

## Phase 1 Parallelism Summary

```
Can run fully in parallel:
  Task 1.1 (signals)
  Task 1.2 (repo)
  Task 1.6 (diff parser)
  Task 1.7 (tmux session model)
  Task 1.8 (dismissed PR)
  Task 1.9 (sync manager)

After 1.1 + 1.2:
  Task 1.3 (branch) — needs signals + repo

After 1.2 + 1.3:
  Task 1.4 (worktree) — needs repo + branch

After 1.1 + 1.2 + 1.3:
  Task 1.5 (pull request) — needs signals + repo + branch
```

---

## Phase 2: Internal Services (parallelizable after Phase 1)

These tasks build the service layer on top of the models and stores.

### Task 2.1: gitCLI — internal wrapper

**Files:**
- `internal/git/gitcli.go`
- `internal/git/gitcli_test.go`

**Work:**
- Rename existing `internal/git/gitservice.go` to `internal/git/gitcli.go`
- Rename `GitService` struct to `gitCLI` (unexported)
- Make all methods unexported (lowercase)
- Preserve all existing functionality
- Add `remoteURL(ctx, repoPath) (string, error)` — `git remote get-url origin`
- Add `parseRepoFullName(remoteURL string) (owner, repo string, err error)` — parse owner/repo from URL

**Tests:**
- `parseRepoFullName` handles HTTPS URLs (`https://github.com/owner/repo.git`)
- `parseRepoFullName` handles SSH URLs (`git@github.com:owner/repo.git`)
- `parseRepoFullName` handles URLs without `.git` suffix
- `parseRepoFullName` returns error for invalid URLs
- Existing test coverage remains passing (updated for unexported methods)

**Dependencies:** Phase 1 complete (specifically Task 1.2, 1.3, 1.4 for types)

---

### Task 2.2: GitService — public facade (repo + branch + worktree)

**Files:**
- `internal/git/gitservice.go`
- `internal/git/gitservice_test.go`

**Work:**
- Define `GitService` struct with all internal dependencies
- Implement repo operations: `FindOrCreateRepo`, `GetRepo`, `GetRepoByPath`
- Implement branch operations: `FindOrCreateBranch`, `CreateBranch`, `GetBranch`, `ListBranches`, `EnsureWorktree`, `GetStartDir`, `SyncBranch`, `CleanupBranch`, `HasWorktree`, `IsHealthy`
- `EnsureWorktree`: checks if worktree exists → if not, pulls branch if needed, creates worktree, inserts Worktree record → returns path
- `SyncBranch`: checks local/remote existence, dirty state, worktree existence via gitCLI calls → updates Branch record + creates/deletes Worktree record as needed
- `CleanupBranch`: removes worktree (CLI + delete record), optionally deletes branch (CLI + update record)

**Tests (in-memory SQLite + mock gitCLI):**
- `FindOrCreateRepo` creates Repo with correct FullName parsed from remote
- `FindOrCreateRepo` returns existing Repo if path matches
- `CreateBranch` creates Branch + Worktree records, calls gitCLI
- `EnsureWorktree` creates worktree when none exists
- `EnsureWorktree` returns existing path when worktree already exists
- `SyncBranch` updates ExistsLocal/ExistsRemote/IsDirty correctly
- `SyncBranch` creates Worktree record when worktree found on disk
- `SyncBranch` deletes Worktree record when worktree no longer on disk
- `CleanupBranch` removes worktree and deletes Worktree record
- `CleanupBranch` with deleteBranch=true also deletes branch
- `GetStartDir` returns worktree path when worktree exists
- `GetStartDir` returns repo path when no worktree
- `IsHealthy` returns true when branch and worktree are consistent
- `IsHealthy` returns false when worktree has wrong branch
- `HasWorktree` returns true/false correctly

**Dependencies:** Task 1.2, 1.3, 1.4, Task 2.1

---

### Task 2.3: GitHub client

**Files:**
- `internal/git/githubclient.go`
- `internal/git/githubclient_test.go`

**Work:**
- Define `GitHubClient` interface
- Implement `githubV4Client` using `shurcooL/githubv4`
- GraphQL queries: list repo PRs, list assigned PRs, get single PR, get current user
- REST call for diff: `GET /repos/{owner}/{repo}/pulls/{number}` with `Accept: application/vnd.github.diff`
- Auth token resolution: `GITHUB_TOKEN` env → `gh auth token` → nil (degraded mode)
- Define `RawPR` struct for API responses, with conversion to `PullRequest` model
- `NewGitHubClient(ctx) (GitHubClient, error)` — resolves token, returns client or nil+error

**Tests (mock HTTP server):**
- `ListRepoPRs` returns parsed PRs from GraphQL response
- `ListAssignedPRs` returns assigned PRs
- `GetPR` returns single PR
- `GetPRDiff` returns raw diff string
- `GetCurrentUser` returns login
- Auth resolution: env var takes precedence
- Auth resolution: falls back to gh CLI
- Auth resolution: returns error when neither available
- Handles API errors gracefully

**Dependencies:** Task 1.5 (for PullRequest model types)

---

### Task 2.4: GitService — PR operations

**Files:**
- `internal/git/gitservice.go` (extend)
- `internal/git/gitservice_pr_test.go`

**Work:**
- Add PR operations to GitService: `GetPR`, `SearchPRs`, `GetPRDiff`, `GetPRsForBranch`
- Add sync operations: `SyncRepoPRs`, `SyncAssignedPRs`, `SyncBranches`
- `SyncRepoPRs`: fetch open PRs from GitHub → for each: find/create Branch for head branch → upsert PR → detect state changes → publish events
- `SyncAssignedPRs`: fetch assigned PRs → match to repos → find/create branches → upsert PRs → return newly discovered
- `SearchPRs`: apply filters at store level (RepoID, BranchID, State) and GitHub level (AssignedTo, Search)
- `GetPRDiff`: load PR from store → call githubClient.GetPRDiff → parse with `ParseUnifiedDiff` → return structured diff

**Tests (in-memory SQLite + mock GitHubClient):**
- `SyncRepoPRs` creates new PR records from API response
- `SyncRepoPRs` updates existing PR records
- `SyncRepoPRs` publishes `pr_state_changed` event on state transition
- `SyncRepoPRs` publishes `pr_discovered` event for new PRs
- `SyncRepoPRs` creates Branch records for unknown head branches
- `SyncAssignedPRs` returns only newly discovered PRs
- `SyncAssignedPRs` skips PRs already in store
- `SearchPRs` filters by repo, branch, state
- `GetPRsForBranch` returns correct PRs
- `GetPRDiff` returns structured diff
- `SyncBranches` calls SyncBranch for each tracked branch

**Dependencies:** Task 2.2, Task 2.3, Task 1.5, Task 1.6

---

### Task 2.5: Consolidated TmuxService

**Files:**
- `internal/tmux/tmuxservice.go` (rewrite)
- `internal/tmux/tmuxclient.go` (remove — functionality absorbed into service)
- `internal/tmux/tmuxservice_test.go`

**Work:**
- Rewrite `TmuxService` to hold `*gotmux.Tmux` directly (no `TmuxClient` interface layer)
- Add `*TmuxStore` dependency for persistence
- `CreateSession(name, startDir, env)` → creates tmux session via gotmux + inserts TmuxSession DB record → returns `*TmuxSession`
- `KillSession(id)` → kills tmux session + deletes DB record
- `RecreateSession(id)` → loads TmuxSession from DB → creates tmux session with stored name/startDir/env → sets IsAlive=true
- `HasSession(name)` → live check via gotmux
- `SwitchClient(targetSession)` → delegates to gotmux
- Hook handlers: update `IsAlive` on DB record, publish events
- Remove `TmuxClient` interface
- For testability: accept gotmux instance as parameter (nil = tmux not available). Tests can provide a mock or nil.
- `GetCurrentSessionName`, `SyncWindows`, `GetWindows`, `ListSessionNames` — preserved with gotmux direct calls

**Tests (in-memory SQLite, nil gotmux for DB-only tests):**
- CreateSession persists TmuxSession record with correct fields
- KillSession deletes record
- RecreateSession loads record and sets IsAlive=true
- HandleSessionClosed sets IsAlive=false and publishes event
- HandleSessionCreated sets IsAlive=true and publishes event
- GetSession/GetSessionByName load correctly
- Service works gracefully when gotmux is nil (returns ErrTmuxNotAvailable)

**Dependencies:** Task 1.7

---

### Task 2.6: GitModule wiring

**Files:**
- `internal/git/gitmodule.go`
- `internal/git/gitmodule_test.go`

**Work:**
- Define `GitModule` struct exposing `Service *GitService`
- `NewGitModule(database, bus, cfg)` — creates all stores, gitCLI, GitHubClient, GitService
- Implement `Module` interface: `OnAppStart`, `OnAppEnd`, `Routes`
- `Routes()` returns empty router (no API endpoints)
- Implement `ModelProvider`: returns `[]any{&Repo{}, &Branch{}, &Worktree{}, &PullRequest{}}`
- `RegisterSyncTasks(manager)` — registers `git.prs` and `git.branches` tasks

**Tests:**
- Module initializes without error
- Models() returns all four models
- RegisterSyncTasks registers expected task names

**Dependencies:** Task 2.2, Task 2.4

---

### Task 2.7: TmuxModule update

**Files:**
- `internal/tmux/tmuxmodule.go` (update)
- `internal/tmux/tmuxmodule_test.go`

**Work:**
- Update `NewTmuxModule` to accept `db.Database` in addition to eventbus
- Create gotmux client internally (instead of accepting TmuxClient)
- Create TmuxStore from database
- Pass store to TmuxService
- Implement `ModelProvider`: returns `[]any{&TmuxSession{}}`
- Remove TmuxClient from constructor signature

**Tests:**
- Module initializes without error
- Models() returns TmuxSession

**Dependencies:** Task 2.5

---

## Phase 2 Parallelism Summary

```
Can run in parallel after Phase 1:
  Task 2.1 (gitCLI rename)
  Task 2.3 (GitHub client)
  Task 2.5 (consolidated tmux service)

After 2.1:
  Task 2.2 (GitService facade) — needs gitCLI

After 2.2 + 2.3:
  Task 2.4 (GitService PR ops) — needs facade + GitHub client

After 2.2 + 2.4:
  Task 2.6 (GitModule) — needs complete GitService

After 2.5:
  Task 2.7 (TmuxModule update) — needs consolidated service
```

---

## Phase 3: Module Integration

These tasks wire the new modules together and update existing modules.

### Task 3.1: Workspace module update

**Files:**
- `internal/workspace/workspace.go`
- `internal/workspace/workspaceservice.go`
- `internal/workspace/workspacemodule.go`
- `internal/workspace/workspaceservice_test.go`

**Work:**
- Replace `IsGitRepo bool` with `RepoID *uint` + `Repo *git.Repo` relationship on Workspace model
- Update `NewWorkspaceModule` to accept `*git.GitModule`
- Module init extracts `gitModule.Service` and passes to `WorkspaceService`
- `CreateWorkspace`: if path is a git repo (checked via gitCLI or stat), call `gitService.FindOrCreateRepo(path)` and set `RepoID`
- Remove internal `GitService` field on WorkspaceModule
- Update all code that checked `ws.IsGitRepo` to check `ws.RepoID != nil`
- Branch listing endpoint delegates to `gitService.ListBranches(repoID)` instead of internal git service

**Tests:**
- CreateWorkspace for git repo sets RepoID
- CreateWorkspace for non-git dir leaves RepoID nil
- `RepoID != nil` correctly replaces IsGitRepo checks
- ListBranches delegates to git module

**Dependencies:** Task 2.6 (GitModule complete)

---

### Task 3.2: Session model update

**Files:**
- `internal/session/session.go`
- `internal/session/sessionstore.go`
- `internal/session/sessionstore_test.go`

**Work:**
- Add new statuses: `StatusPending`, `StatusActive`, `StatusInactive`, `StatusArchived`
- Rename `StatusReady` → `StatusActive` (or add as alias during migration)
- Remove fields: `TmuxSessionName`, `Branch`, `BaseBranch`, `WorktreePath`, `Resources`
- Add fields: `BranchID *uint`, `TmuxSessionID *uint`
- Add relationships: `GitBranch *git.Branch`, `TmuxSession *tmux.TmuxSession`
- Update `SessionStore.GetByID` to Joins GitBranch and TmuxSession
- Update `SessionStore.List` to Joins GitBranch and TmuxSession
- Remove `GetByTmuxName` (replaced by looking up TmuxSession by name, then finding session by TmuxSessionID)
- Add `GetByBranchID(branchID uint) (*Session, error)`
- Add `GetByTmuxSessionID(tmuxSessionID uint) (*Session, error)`
- Remove resources.go (Resources struct no longer needed)

**Tests:**
- All new statuses are valid
- BranchID FK loads GitBranch via Joins
- TmuxSessionID FK loads TmuxSession via Joins
- GetByBranchID returns correct session
- GetByTmuxSessionID returns correct session
- Nullable FKs work (pending session has nil BranchID/TmuxSessionID initially, or just nil TmuxSessionID)
- List loads all relationships correctly

**Dependencies:** Task 1.3 (Branch model), Task 1.7 (TmuxSession model)

---

### Task 3.3: Session service rewrite

**Files:**
- `internal/session/sessionservice.go`
- `internal/session/sessionservice_test.go`

**Work:**
- Accept `*git.GitModule` and `*tmux.TmuxModule` (modules, not services) in constructor
- Extract services internally: `gitMod.Service`, `tmuxMod.Service`
- **Rewrite `CreateSession`**: creates session record with BranchID (find/create Branch via git module). For pending sessions, just creates the record. For immediate sessions, sets status=creating and launches async setup.
- **Implement async `ActivateSession`**: always returns 202. Launches background goroutine. Handles pending (full setup), inactive (tmux recreation), active (client switch).
- **Implement `ArchiveSession`**: validates active/inactive, calls `gitService.CleanupBranch`, calls `tmuxService.KillSession`, sets archived
- **Implement `DismissSession`**: validates pending, creates DismissedPR record, deletes session
- **Rewrite tmux event handlers**:
  - `handleTmuxSessionClosed`: find session by TmuxSessionID → set status=inactive (not broken)
  - `handleTmuxSessionCreated`: find session by TmuxSessionID → if inactive, set active
  - `handleTmuxClientSessionChanged`: update IsAttached flags
- **Rewrite `RefreshSession`** → `ReconcileSession`: checks git health via `gitService.IsHealthy`, checks tmux via TmuxSession.IsAlive. Transitions as needed.
- **Rewrite `RepairSession`**: sets creating, launches async setup to fix git state
- **Rewrite `DeleteSession`**: cleanup git + tmux, set deleted
- **Subscribe to `git.pr_discovered`**: check for existing session with matching BranchID, check DismissedPR, create pending session if appropriate
- **RegisterSyncTasks**: register `session.reconcile` task

**Tests (in-memory SQLite + mock git/tmux services):**
- CreateSession creates record with correct BranchID
- CreateSession for pending PR creates pending session
- ActivateSession from pending → creating → (mock setup) → active
- ActivateSession from inactive → creating → active (tmux recreated)
- ActivateSession from active → switches client
- ActivateSession from broken/archived → error
- ArchiveSession from active → cleans up, sets archived
- ArchiveSession from inactive → sets archived
- ArchiveSession from pending → error
- DismissSession from pending → creates DismissedPR, deletes session
- DismissSession from non-pending → error
- handleTmuxSessionClosed → session goes inactive (NOT broken)
- handleTmuxSessionCreated → inactive session goes active
- handleTmuxClientSessionChanged → IsAttached flags updated
- ReconcileSession: active + tmux dead → inactive
- ReconcileSession: active + worktree gone → broken
- ReconcileSession: inactive + worktree gone → broken
- git.pr_discovered creates pending session when no existing session for branch
- git.pr_discovered skips when session exists for branch
- git.pr_discovered skips when PR is dismissed
- DeleteSession cleans up all resources
- RepairSession from broken → creating → re-runs setup

**Dependencies:** Task 3.2, Task 2.6, Task 2.7, Task 1.8

---

### Task 3.4: Session controller + router update

**Files:**
- `internal/session/sessioncontroller.go`
- `internal/session/sessionrouter.go`
- `internal/session/sessioncontroller_test.go`

**Work:**
- Update response building: load PRs via `gitService.GetPRsForBranch(branchID)`, aggregate signals from all associated models
- `ActivateSession` handler returns 202 for all cases
- Add `ArchiveSession` handler (PUT /sessions/{id}/archive)
- Add `DismissSession` handler (PUT /sessions/{id}/dismiss)
- Update `CreateSession` request to accept `branch_id` or `branch_name` + `workspace_id` (resolve branch via git module)
- Response includes: session fields, branch info, PRs, tmux session, signals, top_severity

**Tests (httptest):**
- GET /sessions returns sessions with signals and PR info
- GET /sessions/{id} returns full detail with signals
- PUT /sessions/{id}/activate returns 202
- PUT /sessions/{id}/archive returns 200 for active session
- PUT /sessions/{id}/archive returns 400 for pending session
- PUT /sessions/{id}/dismiss returns 200 for pending session
- PUT /sessions/{id}/dismiss returns 400 for active session
- POST /sessions creates session correctly
- Response shape matches spec (signals, branch, PRs, tmux_session)

**Dependencies:** Task 3.3

---

### Task 3.5: Session module update

**Files:**
- `internal/session/sessionmodule.go`

**Work:**
- Update `NewSessionModule` to accept module references: `*git.GitModule`, `*tmux.TmuxModule`, `*workspace.WorkspaceModule`, `*claude.ClaudeModule`
- Module init extracts services and passes to SessionService and SessionController
- `Models()` returns `[]any{&Session{}, &DismissedPR{}}`
- Add `RegisterSyncTasks(manager *sync.SyncManager)` method

**Dependencies:** Task 3.3, Task 3.4

---

### Task 3.6: Claude module signal method

**Files:**
- `internal/claude/claudesession.go` (add method)
- `internal/claude/signals_test.go`

**Work:**
- Add `Signals() []common.Signal` method to `ClaudeSession`
- Map existing statuses: NeedsAttention → urgent, Working → active, ReadyForReview → warning, Done → success, Idle → info

**Tests:**
- Each ClaudeSession status maps to correct signal severity
- Signal key includes claude session ID
- Signal label is human-readable

**Dependencies:** Task 1.1 (Signal types)

---

### Task 3.7: App wiring update

**Files:**
- `internal/api/app.go`
- `internal/api/config.go`

**Work:**
- Add `Git *git.GitModule` and `Sync *sync.SyncManager` to App struct
- Update `newApp`:
  - Create `gitModule` (before workspace/session)
  - Update `tmuxModule` creation (pass database, remove TmuxClient parameter)
  - Update `workspaceModule` creation (pass gitModule)
  - Update `sessionModule` creation (pass all modules)
  - Create `syncManager`, register tasks, add to lifecycle
- Add `gitModule` to `modules()` list
- Update `OnStart`: start sync manager after all modules
- Update `OnEnd`: stop sync manager before stopping modules
- Add config fields: `GitHubSyncInterval`, `BranchSyncInterval`, `SessionReconcileInterval`, `GitHubEnabled`

**Dependencies:** Task 2.6, Task 2.7, Task 3.5, Task 1.9

---

## Phase 3 Parallelism Summary

```
Can run in parallel after Phase 2:
  Task 3.1 (workspace update) — needs GitModule
  Task 3.2 (session model) — needs Branch + TmuxSession models
  Task 3.6 (claude signals) — needs Signal types only

After 3.2 + Phase 2:
  Task 3.3 (session service) — needs everything

After 3.3:
  Task 3.4 (session controller)

After 3.3 + 3.4:
  Task 3.5 (session module)

After all:
  Task 3.7 (app wiring)
```

---

## Phase 4: Integration Tests

### Task 4.1: End-to-end session lifecycle tests

**Files:**
- `internal/session/integration_test.go`

**Work:**
- Test full session lifecycle with in-memory SQLite and mock git/tmux:
  - Create session → creating → active
  - Kill tmux → inactive
  - Activate inactive → creating → active (tmux recreated)
  - Archive → archived
  - Delete → deleted
- Test pending session lifecycle:
  - PR discovered → pending session created
  - Activate pending → creating → active
  - Dismiss pending → deleted + DismissedPR created
- Test broken session:
  - Worktree disappears → broken
  - Repair → creating → active
- Test signal aggregation in API responses

**Dependencies:** Task 3.7 (full app wiring)

---

### Task 4.2: Git sync integration tests

**Files:**
- `internal/git/sync_integration_test.go`

**Work:**
- Test PR sync with mock GitHub client:
  - New PRs create Branch + PR records
  - State changes publish events
  - Assigned PRs published as discovered
- Test branch sync:
  - SyncBranch updates state correctly
  - Missing worktree deletes Worktree record

**Dependencies:** Task 2.6

---

### Task 4.3: Sync manager integration tests

**Files:**
- `internal/sync/integration_test.go`

**Work:**
- Register multiple tasks, verify they run at intervals
- Verify TriggerSync works
- Verify graceful shutdown

**Dependencies:** Task 1.9, Task 3.7

---

## Full Dependency Graph

```
Phase 1 (Foundation):
  1.1 ─────────────────────────────────┐
  1.2 ──────────┬──────────┬───────────┤
  1.6 ──────────│──────────│───────────┤
  1.7 ──────────│──────────│───────────┤
  1.8 ──────────│──────────│───────────┤
  1.9 ──────────│──────────│───────────┤
                │          │           │
  1.3 ◄─── 1.1 + 1.2      │           │
                │          │           │
  1.4 ◄─── 1.2 + 1.3      │           │
                │          │           │
  1.5 ◄─── 1.1 + 1.2 + 1.3            │
                                       │
Phase 2 (Services):                    │
  2.1 ◄─── Phase 1                     │
  2.3 ◄─── 1.5                        │
  2.5 ◄─── 1.7                        │
                                       │
  2.2 ◄─── 2.1 + 1.2-1.4             │
  2.7 ◄─── 2.5                        │
                                       │
  2.4 ◄─── 2.2 + 2.3 + 1.5 + 1.6    │
                                       │
  2.6 ◄─── 2.2 + 2.4                  │
                                       │
Phase 3 (Integration):                │
  3.1 ◄─── 2.6                        │
  3.2 ◄─── 1.3 + 1.7                  │
  3.6 ◄─── 1.1                        │
                                       │
  3.3 ◄─── 3.2 + 2.6 + 2.7 + 1.8    │
  3.4 ◄─── 3.3                        │
  3.5 ◄─── 3.3 + 3.4                  │
                                       │
  3.7 ◄─── 2.6 + 2.7 + 3.5 + 1.9    │
                                       │
Phase 4 (Integration Tests):          │
  4.1 ◄─── 3.7                        │
  4.2 ◄─── 2.6                        │
  4.3 ◄─── 1.9 + 3.7                  │
```

---

## Task Summary

| Task | Name | Estimated Scope | Can Parallel With |
|------|------|----------------|-------------------|
| 1.1 | Signal types | Small | 1.2, 1.6, 1.7, 1.8, 1.9 |
| 1.2 | Repo model + store | Small | 1.1, 1.6, 1.7, 1.8, 1.9 |
| 1.3 | Branch model + store | Small | 1.4*, 1.6, 1.7, 1.8, 1.9 |
| 1.4 | Worktree model + store | Small | 1.5*, 1.6, 1.7, 1.8, 1.9 |
| 1.5 | PullRequest model + store | Small | 1.6, 1.7, 1.8, 1.9 |
| 1.6 | Diff parser | Medium | 1.1-1.5, 1.7, 1.8, 1.9 |
| 1.7 | TmuxSession model + store | Small | 1.1-1.6, 1.8, 1.9 |
| 1.8 | DismissedPR model + store | Small | 1.1-1.7, 1.9 |
| 1.9 | SyncManager | Medium | 1.1-1.8 |
| 2.1 | gitCLI rename | Small | 2.3, 2.5 |
| 2.2 | GitService facade | Large | 2.3, 2.5 |
| 2.3 | GitHub client | Medium | 2.1, 2.2, 2.5 |
| 2.4 | GitService PR ops | Medium | 2.5, 2.7 |
| 2.5 | Consolidated TmuxService | Medium | 2.1, 2.2, 2.3 |
| 2.6 | GitModule wiring | Small | 2.7 |
| 2.7 | TmuxModule update | Small | 2.6 |
| 3.1 | Workspace module update | Medium | 3.2, 3.6 |
| 3.2 | Session model update | Medium | 3.1, 3.6 |
| 3.3 | Session service rewrite | Large | — |
| 3.4 | Session controller update | Medium | — |
| 3.5 | Session module update | Small | — |
| 3.6 | Claude signals | Small | 3.1, 3.2 |
| 3.7 | App wiring | Medium | — |
| 4.1 | E2E session tests | Medium | 4.2, 4.3 |
| 4.2 | Git sync tests | Medium | 4.1, 4.3 |
| 4.3 | Sync manager tests | Small | 4.1, 4.2 |

**Total: 25 tasks across 4 phases**

*Maximum parallelism: up to 6 tasks simultaneously in Phase 1*
