# PR Sync Consolidation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Consolidate PR sync into a single method, auto-create pending sessions for assigned PRs, mark sessions completed on PR merge, and auto-archive stale completed sessions.

**Architecture:** `SyncRepoPRs` becomes the single PR sync path, using a `syncRawPR` helper. The session service reacts to PR events: `PRDiscovered` creates pending sessions for assigned PRs, `PRStateChanged` marks sessions completed on merge. A cleanup job archives completed sessions after 5 minutes of inactivity.

**Tech Stack:** Go, GORM/SQLite, eventbus

---

### Task 1: Add `Assignees` to `RawPR` and `IsAssignedToMe` to `PullRequest`

**Files:**
- Modify: `internal/git/githubclient.go:23-42` (RawPR struct)
- Modify: `internal/git/pullrequest.go:22-33` (PullRequest struct)
- Modify: `internal/git/gitservice.go:31-39` (GitService struct)
- Modify: `internal/git/gitservice.go:479-490` (rawPRToPullRequest)
- Test: `internal/git/gitservice_pr_test.go`

**Step 1: Write failing test**

Add to `internal/git/gitservice_pr_test.go`:

```go
func TestSyncRepoPRs_SetsIsAssignedToMe(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	raw := makeRawPR(1, "Assigned PR", "feature-a", "open", false)
	raw.Assignees = []struct {
		Login string `json:"login"`
	}{{Login: "myself"}}
	ghClient := &mockGitHubClient{repoPRs: []RawPR{raw}}
	svc := NewGitService(database, WithGitHubClient(ghClient))
	svc.currentUser = "myself"

	err := svc.SyncRepoPRs(context.Background(), repo)
	require.NoError(t, err)

	prs := svc.prStore.ListByRepo(repo.ID)
	require.Len(t, prs, 1)
	assert.True(t, prs[0].IsAssignedToMe)
}

func TestSyncRepoPRs_NotAssigned(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	raw := makeRawPR(1, "Someone elses PR", "feature-b", "open", false)
	raw.Assignees = []struct {
		Login string `json:"login"`
	}{{Login: "someone-else"}}
	ghClient := &mockGitHubClient{repoPRs: []RawPR{raw}}
	svc := NewGitService(database, WithGitHubClient(ghClient))
	svc.currentUser = "myself"

	err := svc.SyncRepoPRs(context.Background(), repo)
	require.NoError(t, err)

	prs := svc.prStore.ListByRepo(repo.ID)
	require.Len(t, prs, 1)
	assert.False(t, prs[0].IsAssignedToMe)
}
```

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `RawPR` has no `Assignees` field, `PullRequest` has no `IsAssignedToMe`, `GitService` has no `currentUser`

**Step 3: Implement model changes**

In `internal/git/githubclient.go:23-42`, add `Assignees` to `RawPR`:

```go
type RawPR struct {
	Number    int     `json:"number"`
	Title     string  `json:"title"`
	State     string  `json:"state"`
	Draft     bool    `json:"draft"`
	HTMLURL   string  `json:"html_url"`
	MergedAt  *string `json:"merged_at"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		Ref  string `json:"ref"`
		Repo *struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}
```

In `internal/git/pullrequest.go:22-33`, add `IsAssignedToMe`:

```go
type PullRequest struct {
	gorm.Model
	RepoID         uint    `json:"repo_id" gorm:"uniqueIndex:idx_pr_repo_number;index"`
	Number         int     `json:"number" gorm:"uniqueIndex:idx_pr_repo_number"`
	HeadBranchID   *uint   `json:"head_branch_id,omitempty" gorm:"index"`
	HeadBranch     *Branch `json:"head_branch,omitempty" gorm:"foreignKey:HeadBranchID"`
	Title          string  `json:"title"`
	State          PRState `json:"state"`
	IsDraft        bool    `json:"is_draft"`
	IsAssignedToMe bool    `json:"is_assigned_to_me"`
	HTMLURL        string  `json:"html_url"`
	AuthorLogin    string  `json:"author_login"`
}
```

In `internal/git/gitservice.go:31-39`, add `currentUser` field:

```go
type GitService struct {
	cli           *gitCLI
	repoStore     *RepoStore
	branchStore   *BranchStore
	worktreeStore *WorktreeStore
	prStore       *PRStore
	githubClient  GitHubClient
	eventBus      eventbus.EventBus
	currentUser   string
}
```

In `internal/git/gitservice.go:479-490`, update `rawPRToPullRequest` to accept `currentUser`:

```go
func rawPRToPullRequest(raw RawPR, repoID uint, branchID uint, currentUser string) *PullRequest {
	assigned := false
	for _, a := range raw.Assignees {
		if a.Login == currentUser {
			assigned = true
			break
		}
	}
	return &PullRequest{
		RepoID:         repoID,
		Number:         raw.Number,
		HeadBranchID:   &branchID,
		Title:          raw.Title,
		State:          raw.ToPRState(),
		IsDraft:        raw.Draft,
		IsAssignedToMe: assigned,
		HTMLURL:        raw.HTMLURL,
		AuthorLogin:    raw.User.Login,
	}
}
```

Update all call sites of `rawPRToPullRequest` in `SyncRepoPRs` (line 397) and `SyncAssignedPRs` (line 463) to pass `s.currentUser`.

**Step 4: Run tests**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```
feat(git): add assignee tracking to PR sync

Add Assignees to RawPR, IsAssignedToMe to PullRequest model,
and currentUser to GitService for assignment detection.
```

---

### Task 2: Resolve current user on startup

**Files:**
- Modify: `internal/git/gitmodule.go:25-35` (OnAppStart)
- Test: `internal/git/gitmodule_test.go`

**Step 1: Write failing test**

In `internal/git/gitmodule_test.go`, add a test that verifies `currentUser` is set after `OnAppStart`:

```go
func TestGitModule_OnAppStart_SetsCurrentUser(t *testing.T) {
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	bus := &mockEventBus{}
	module := NewGitModule(database, bus)
	module.Service.githubClient = &mockGitHubClient{currentUser: "testuser"}

	err = module.OnAppStart(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "testuser", module.Service.currentUser)
}
```

Check if `gitmodule_test.go` already imports the test helpers — it likely uses the same `mockGitHubClient` from `gitservice_pr_test.go`. Since they're in the same package, this should work.

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `OnAppStart` doesn't set `currentUser`

**Step 3: Implement**

In `internal/git/gitmodule.go:25-35`, update `OnAppStart`:

```go
func (m *GitModule) OnAppStart(ctx context.Context) error {
	if m.Service.githubClient != nil {
		return nil
	}
	ghClient, err := NewGitHubClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize GitHub client: %w", err)
	}
	m.Service.githubClient = ghClient

	user, err := ghClient.GetCurrentUser(ctx)
	if err != nil {
		slog.Warn("failed to get current GitHub user", "error", err)
	} else {
		m.Service.currentUser = user
	}

	return nil
}
```

Note: when `githubClient` is already set (test/injection path), we should still resolve the user. Adjust the logic:

```go
func (m *GitModule) OnAppStart(ctx context.Context) error {
	if m.Service.githubClient == nil {
		ghClient, err := NewGitHubClient(ctx)
		if err != nil {
			return fmt.Errorf("failed to initialize GitHub client: %w", err)
		}
		m.Service.githubClient = ghClient
	}

	user, err := m.Service.githubClient.GetCurrentUser(ctx)
	if err != nil {
		slog.Warn("failed to get current GitHub user", "error", err)
	} else {
		m.Service.currentUser = user
	}

	return nil
}
```

**Step 4: Run tests**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```
feat(git): resolve current GitHub user on startup
```

---

### Task 3: Extract `syncRawPR` and consolidate `SyncRepoPRs`

**Files:**
- Modify: `internal/git/gitservice.go:372-477` (SyncRepoPRs, SyncAssignedPRs)
- Test: `internal/git/gitservice_pr_test.go`

**Step 1: Verify existing tests pass before refactor**

Run: `task test`
Expected: PASS — all existing `TestSyncRepoPRs_*` tests still pass

**Step 2: Extract `syncRawPR` helper**

In `internal/git/gitservice.go`, add a new method and refactor `SyncRepoPRs` to use it:

```go
func (s *GitService) syncRawPR(ctx context.Context, raw RawPR, repo *Repo) error {
	branch, _ := s.branchStore.GetByNameAndRepo(raw.Head.Ref, repo.ID)
	if branch == nil {
		branch = &Branch{Name: raw.Head.Ref, RepoID: repo.ID, ExistsRemote: true}
		if err := s.branchStore.Upsert(branch); err != nil {
			return fmt.Errorf("failed to upsert branch %s: %w", raw.Head.Ref, err)
		}
	}

	existing, _ := s.prStore.GetByRepoAndNumber(repo.ID, raw.Number)
	pr := rawPRToPullRequest(raw, repo.ID, branch.ID, s.currentUser)

	if existing != nil {
		oldState := existing.State
		pr.Model = existing.Model
		if err := s.prStore.Update(pr); err != nil {
			return fmt.Errorf("failed to update PR #%d: %w", raw.Number, err)
		}
		if oldState != pr.State && s.eventBus != nil {
			s.eventBus.Publish(ctx, eventbus.Event{
				Type: EventPRStateChanged,
				Data: PRStateChangedEvent{PullRequest: pr, OldState: oldState, NewState: pr.State},
			})
		}
	} else {
		if err := s.prStore.Upsert(pr); err != nil {
			return fmt.Errorf("failed to upsert PR #%d: %w", raw.Number, err)
		}
		if s.eventBus != nil {
			s.eventBus.Publish(ctx, eventbus.Event{
				Type: EventPRDiscovered,
				Data: PRDiscoveredEvent{PullRequest: pr, Repo: repo},
			})
		}
	}
	return nil
}

func (s *GitService) SyncRepoPRs(ctx context.Context, repo *Repo) error {
	if s.githubClient == nil {
		return ErrNoGitHubClient
	}
	owner, name, err := repo.OwnerAndName()
	if err != nil {
		return err
	}
	rawPRs, err := s.githubClient.ListRepoPRs(ctx, owner, name)
	if err != nil {
		return err
	}
	var errs []error
	for _, raw := range rawPRs {
		if err := s.syncRawPR(ctx, raw, repo); err != nil {
			slog.Warn("failed to sync PR", "number", raw.Number, "error", err)
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("SyncRepoPRs: %d errors (first: %w)", len(errs), errs[0])
	}
	return nil
}
```

**Step 3: Run tests to verify refactor is behavior-preserving**

Run: `task test`
Expected: PASS — all `TestSyncRepoPRs_*` tests still pass with identical behavior

**Step 4: Commit**

```
refactor(git): extract syncRawPR helper from SyncRepoPRs
```

---

### Task 4: Remove `SyncAssignedPRs` and `ListAssignedPRs`

**Files:**
- Modify: `internal/git/gitservice.go` — delete `SyncAssignedPRs` (lines 433-477)
- Modify: `internal/git/githubclient.go` — remove `ListAssignedPRs` from interface (line 17) and implementation (lines 128-172)
- Modify: `internal/git/gitmodule.go:56-75` — remove assigned PR call from `prSyncTask.Run`
- Modify: `internal/git/gitservice_pr_test.go` — delete `TestSyncAssignedPRs_*` tests (lines 215-253) and `mockGitHubClient.assignedPRs` field
- Modify: `internal/git/githubclient_test.go` — delete `TestListAssignedPRs` (lines 195-240)

**Step 1: Remove `ListAssignedPRs` from interface and implementation**

In `internal/git/githubclient.go`:
- Remove `ListAssignedPRs(ctx context.Context) ([]RawPR, error)` from the `GitHubClient` interface (line 17)
- Delete the `ListAssignedPRs` method on `githubRESTClient` (lines 128-172)

**Step 2: Remove `SyncAssignedPRs` from `GitService`**

Delete the entire `SyncAssignedPRs` method from `internal/git/gitservice.go` (lines 433-477).

**Step 3: Remove assigned PR call from `prSyncTask`**

In `internal/git/gitmodule.go:56-75`, update `prSyncTask.Run` to only sync repo PRs:

```go
func (t *prSyncTask) Run(ctx context.Context) error {
	repos := t.service.repoStore.List()
	for _, repo := range repos {
		slog.Info("syncing PRs for repo", "repo", repo.FullName)
		if err := t.service.SyncRepoPRs(ctx, &repo); err != nil {
			if errors.Is(err, ErrNoGitHubClient) {
				return err
			}
			slog.Warn("failed to sync PRs for repo", "repo", repo.FullName, "error", err)
		}
	}
	return nil
}
```

**Step 4: Remove test code**

In `internal/git/gitservice_pr_test.go`:
- Remove the `assignedPRs` field from `mockGitHubClient` (line 15)
- Remove the `ListAssignedPRs` method from `mockGitHubClient` (lines 29-33)
- Delete `TestSyncAssignedPRs_ReturnsNewlyDiscoveredPRs` (lines 215-227)
- Delete `TestSyncAssignedPRs_SkipsAlreadyKnownPRs` (lines 229-253)
- Delete `TestSyncAssignedPRs_NilGitHubClient_ReturnsError` (lines 337-344)

In `internal/git/githubclient_test.go`:
- Delete `TestListAssignedPRs` (lines 195-240)

**Step 5: Run tests**

Run: `task test`
Expected: PASS

**Step 6: Commit**

```
refactor(git): remove SyncAssignedPRs and ListAssignedPRs

Assignment tracking is now handled inline by SyncRepoPRs via the
IsAssignedToMe field on PullRequest.
```

---

### Task 5: Add `StatusCompleted` and `GetByRepoID` workspace query

**Files:**
- Modify: `internal/session/session.go:23-31` (status constants)
- Modify: `internal/workspace/workspacestore.go` (add GetByRepoID)
- Modify: `internal/workspace/workspaceservice.go` (add GetWorkspaceByRepoID)
- Test: `internal/workspace/workspacestore_test.go`

**Step 1: Write failing test for `GetByRepoID`**

Add to `internal/workspace/workspacestore_test.go`:

```go
func TestWorkspaceStore_GetByRepoID(t *testing.T) {
	store := setupWorkspaceStore(t)
	repoID := uint(42)
	ws := &Workspace{Name: "my-ws", Path: "/test/my-ws", IsGitRepo: true, RepoID: &repoID}
	require.NoError(t, store.Add(ws))

	found, err := store.GetByRepoID(repoID)
	require.NoError(t, err)
	assert.Equal(t, ws.ID, found.ID)
	assert.Equal(t, repoID, *found.RepoID)
}

func TestWorkspaceStore_GetByRepoID_NotFound(t *testing.T) {
	store := setupWorkspaceStore(t)
	_, err := store.GetByRepoID(999)
	assert.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `GetByRepoID` doesn't exist

**Step 3: Implement**

Add to `internal/workspace/workspacestore.go` (after `GetByPath`):

```go
func (s *WorkspaceStore) GetByRepoID(repoID uint) (*Workspace, error) {
	var ws Workspace
	if err := s.db.First(&ws, "repo_id = ?", repoID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewNotFound(fmt.Sprintf("workspace not found for repo: %d", repoID))
		}
		return nil, err
	}
	return &ws, nil
}
```

Add to `internal/workspace/workspaceservice.go` (after `GetWorkspaceByPath`):

```go
func (s *WorkspaceService) GetWorkspaceByRepoID(ctx context.Context, repoID uint) (*Workspace, error) {
	return s.store.GetByRepoID(repoID)
}
```

Add `StatusCompleted` to `internal/session/session.go:23-31`:

```go
const (
	StatusCreating  SessionStatus = "creating"
	StatusActive    SessionStatus = "active"
	StatusBroken    SessionStatus = "broken"
	StatusDeleted   SessionStatus = "deleted"
	StatusPending   SessionStatus = "pending"
	StatusInactive  SessionStatus = "inactive"
	StatusArchived  SessionStatus = "archived"
	StatusCompleted SessionStatus = "completed"
)
```

**Step 4: Run tests**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```
feat: add StatusCompleted and workspace GetByRepoID query
```

---

### Task 6: Implement `handlePRDiscovered` — create pending sessions

**Files:**
- Modify: `internal/session/sessionservice.go:786-807` (handlePRDiscovered)
- Test: `internal/session/sessionservice_test.go` (or create if it doesn't exist)

First, check if `internal/session/sessionservice_test.go` exists and what test helpers are available.

**Step 1: Write failing test**

The test needs to set up: a workspace with a repo, a branch, a PR, and fire a `PRDiscoveredEvent`. Then assert a pending session was created.

This test will be integration-style using in-memory SQLite. Look at existing session tests for the pattern. Create the test so that it exercises `handlePRDiscovered` directly.

```go
func TestHandlePRDiscovered_CreatesSession(t *testing.T) {
	// Setup: database, stores, services with mocks
	// Create workspace with RepoID
	// Fire PRDiscoveredEvent with IsAssignedToMe=true
	// Assert: session with StatusPending exists for that branch
}

func TestHandlePRDiscovered_SkipsUnassigned(t *testing.T) {
	// Same setup but IsAssignedToMe=false
	// Assert: no session created
}

func TestHandlePRDiscovered_SkipsExistingSession(t *testing.T) {
	// Setup: session already exists for the branch
	// Fire PRDiscoveredEvent with IsAssignedToMe=true
	// Assert: no new session created
}

func TestHandlePRDiscovered_SkipsDismissed(t *testing.T) {
	// Setup: PR is in dismissed store
	// Fire PRDiscoveredEvent with IsAssignedToMe=true
	// Assert: no session created
}
```

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `handlePRDiscovered` just logs

**Step 3: Implement**

Replace `handlePRDiscovered` in `internal/session/sessionservice.go:786-807`:

```go
func (s *SessionService) handlePRDiscovered(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(git.PRDiscoveredEvent)
	if !ok {
		return nil
	}

	if !data.PullRequest.IsAssignedToMe {
		return nil
	}

	if data.PullRequest.HeadBranchID == nil {
		return nil
	}

	_, err := s.store.GetByBranchID(*data.PullRequest.HeadBranchID)
	if err == nil {
		return nil
	}

	if s.dismissedPRStore != nil && s.dismissedPRStore.IsDismissed(data.PullRequest.ID) {
		return nil
	}

	ws, err := s.workspaceService.GetWorkspaceByRepoID(ctx, data.Repo.ID)
	if err != nil {
		slog.Debug("no workspace for repo, skipping pending session", "repo_id", data.Repo.ID)
		return nil
	}

	branch, err := s.gitService.GetBranch(*data.PullRequest.HeadBranchID)
	if err != nil {
		return fmt.Errorf("failed to get branch: %w", err)
	}

	sess := &Session{
		Name:        branch.Name,
		WorkspaceID: ws.ID,
		BranchID:    data.PullRequest.HeadBranchID,
		Status:      StatusPending,
		LastUsedAt:  time.Now(),
	}

	if err := s.store.Add(sess); err != nil {
		slog.Warn("failed to create pending session for PR", "pr", data.PullRequest.Number, "error", err)
		return nil
	}

	slog.Info("created pending session for assigned PR", "pr", data.PullRequest.Number, "session", sess.ID, "branch", branch.Name)
	return nil
}
```

**Step 4: Run tests**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```
feat(session): create pending sessions for assigned PRs
```

---

### Task 7: Implement `handlePRStateChanged` — complete session on merge

**Files:**
- Modify: `internal/session/sessionservice.go:809-819` (handlePRStateChanged)
- Test: same test file as Task 6

**Step 1: Write failing test**

```go
func TestHandlePRStateChanged_CompletesSessionOnMerge(t *testing.T) {
	// Setup: session exists for a branch, PR exists on that branch
	// Fire PRStateChangedEvent with NewState=PRStateMerged
	// Assert: session status is now StatusCompleted
}

func TestHandlePRStateChanged_IgnoresNonMerge(t *testing.T) {
	// Fire with NewState=PRStateClosed
	// Assert: session status unchanged
}

func TestHandlePRStateChanged_NoSessionForBranch(t *testing.T) {
	// Fire with PRStateMerged but no session for that branch
	// Assert: no error, no crash
}
```

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — handler just logs

**Step 3: Implement**

Replace `handlePRStateChanged` in `internal/session/sessionservice.go:809-819`:

```go
func (s *SessionService) handlePRStateChanged(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(git.PRStateChangedEvent)
	if !ok {
		return nil
	}

	if data.NewState != git.PRStateMerged {
		return nil
	}

	if data.PullRequest.HeadBranchID == nil {
		return nil
	}

	sess, err := s.store.GetByBranchID(*data.PullRequest.HeadBranchID)
	if err != nil {
		return nil
	}

	if sess.Status == StatusDeleted || sess.Status == StatusArchived || sess.Status == StatusCompleted {
		return nil
	}

	sess.Status = StatusCompleted
	if err := s.store.Update(sess); err != nil {
		slog.Warn("failed to mark session completed", "session", sess.ID, "error", err)
	} else {
		slog.Info("marked session completed after PR merge", "session", sess.ID, "pr", data.PullRequest.Number)
	}
	return nil
}
```

**Step 4: Run tests**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```
feat(session): mark sessions completed when PR is merged
```

---

### Task 8: Add `completedCleanupTask`

**Files:**
- Modify: `internal/session/sessionmodule.go` (add task registration)
- Test: `internal/session/sessionmodule.go` (inline test or separate)

**Step 1: Write failing test**

Test that the cleanup task archives completed sessions older than 5 minutes that aren't attached, and leaves recent or attached ones alone.

```go
func TestCompletedCleanupTask_ArchivesStale(t *testing.T) {
	// Setup: in-memory DB, session store, session service
	// Create a session with StatusCompleted, LastUsedAt = 10 min ago, IsAttached = false
	// Run the task
	// Assert: session is now StatusArchived
}

func TestCompletedCleanupTask_SkipsAttached(t *testing.T) {
	// StatusCompleted, LastUsedAt = 10 min ago, IsAttached = true
	// Assert: still StatusCompleted
}

func TestCompletedCleanupTask_SkipsRecent(t *testing.T) {
	// StatusCompleted, LastUsedAt = 1 min ago, IsAttached = false
	// Assert: still StatusCompleted
}
```

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — task doesn't exist

**Step 3: Implement**

Add to `internal/session/sessionmodule.go`:

```go
type completedCleanupTask struct {
	service *SessionService
}

func (t *completedCleanupTask) Name() string            { return "session.completed_cleanup" }
func (t *completedCleanupTask) Interval() time.Duration { return 5 * time.Minute }
func (t *completedCleanupTask) Run(ctx context.Context) error {
	sessions, err := t.service.store.List()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}
	cutoff := time.Now().Add(-5 * time.Minute)
	for _, sess := range sessions {
		if sess.Status == StatusCompleted && !sess.IsAttached && sess.LastUsedAt.Before(cutoff) {
			if _, err := t.service.ArchiveSession(ctx, sess.ID); err != nil {
				slog.Warn("failed to auto-archive completed session", "session", sess.ID, "error", err)
			} else {
				slog.Info("auto-archived completed session", "session", sess.ID)
			}
		}
	}
	return nil
}
```

Update `RegisterJobs` in `internal/session/sessionmodule.go:89-91`:

```go
func (m *SessionModule) RegisterJobs(svc *jobs.JobService) {
	svc.Register(&reconcileSyncTask{service: m.Service})
	svc.Register(&completedCleanupTask{service: m.Service})
}
```

Also update `ArchiveSession` in `internal/session/sessionservice.go:693-719` to accept `StatusCompleted` as a valid source status:

```go
func (s *SessionService) ArchiveSession(ctx context.Context, id uint) (*Session, error) {
	sess, err := s.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	if sess.Status != StatusActive && sess.Status != StatusInactive && sess.Status != StatusCompleted {
		return nil, fmt.Errorf("cannot archive session in status %s", sess.Status)
	}
	// ... rest unchanged
```

**Step 4: Run tests**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```
feat(session): auto-archive completed sessions after 5 min inactivity
```

---

### Task 9: Update `ReconcileSession` for `StatusCompleted`

**Files:**
- Modify: `internal/session/sessionservice.go:743-784` (ReconcileSession)

**Step 1: Verify current behavior**

Currently `ReconcileSession` skips `StatusCreating`, `StatusDeleted`, `StatusArchived`, `StatusPending`. `StatusCompleted` is not skipped, so it will flow through the normal health-check logic. This is correct — a completed session's tmux can still die and should be reconciled.

However, the reconcile logic currently sets `StatusInactive` when tmux dies for `StatusActive` sessions. For `StatusCompleted`, we want to keep it as `StatusCompleted` (not demote to inactive). Update the logic:

```go
if !gitHealthy {
	sess.Status = StatusBroken
	sess.StatusError = "worktree or branch is unhealthy"
} else if !tmuxAlive {
	if sess.Status == StatusActive {
		sess.Status = StatusInactive
	}
} else {
	if sess.Status == StatusInactive {
		sess.Status = StatusActive
	}
}
```

This already works correctly — it only changes `StatusActive` → `StatusInactive` and vice versa. `StatusCompleted` stays `StatusCompleted` unless git is unhealthy. No change needed.

**Step 2: Commit (skip if no change needed)**

If no code change was made, skip this task.

---

### Task 10: Final verification

**Step 1: Run full test suite**

Run: `task test`
Expected: ALL PASS

**Step 2: Run fmt**

Run: `task fmt`

**Step 3: Commit any formatting fixes**

```
chore: format code
```
