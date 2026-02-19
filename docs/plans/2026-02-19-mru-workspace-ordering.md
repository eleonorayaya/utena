# MRU Workspace Ordering Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Sort workspaces in the picker by most recently used, persisting LastUsedAt on the workspace struct.

**Architecture:** Add `LastUsedAt` to `Workspace`, persist metadata in `workspace_metadata.json`, add `Update()` to store, add `Touch()` to service. Session service switches from workspace store to workspace service dependency.

**Tech Stack:** Go, afero (filesystem), testify (assertions)

---

### Task 1: Add `LastUsedAt` to Workspace struct

**Files:**
- Modify: `internal/workspace/workspace.go:3-8`

**Step 1: Write the failing test**

Add to `internal/workspace/workspacestore_test.go`:

```go
func TestWorkspaceStore_List_SortedByLastUsedAt(t *testing.T) {
	store := setupWorkspaceStore(t)

	now := time.Now()
	store.Add(&Workspace{ID: "ws-1", Name: "alpha", Path: "/path1", LastUsedAt: now.Add(-2 * time.Hour)})
	store.Add(&Workspace{ID: "ws-2", Name: "bravo", Path: "/path2", LastUsedAt: now})
	store.Add(&Workspace{ID: "ws-3", Name: "charlie", Path: "/path3"})

	list := store.List()
	require.Len(t, list, 3)
	require.Equal(t, "bravo", list[0].Name, "Most recent should be first")
	require.Equal(t, "alpha", list[1].Name, "Second most recent should be second")
	require.Equal(t, "charlie", list[2].Name, "Never-used should be last")
}
```

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `LastUsedAt` field doesn't exist on `Workspace`

**Step 3: Add the field**

In `internal/workspace/workspace.go`, add the field:

```go
type Workspace struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	IsGitRepo  bool      `json:"is_git_repo"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
}
```

Add `"time"` to the imports.

**Step 4: Update `List()` sort logic**

In `internal/workspace/workspacestore.go`, replace the `List()` sort (lines 85-87) with:

```go
sort.Slice(workspaces, func(i, j int) bool {
	iUsed := !workspaces[i].LastUsedAt.IsZero()
	jUsed := !workspaces[j].LastUsedAt.IsZero()

	if iUsed && jUsed {
		return workspaces[i].LastUsedAt.After(workspaces[j].LastUsedAt)
	}
	if iUsed != jUsed {
		return iUsed
	}
	return workspaces[i].Name < workspaces[j].Name
})
```

**Step 5: Run tests**

Run: `task test`
Expected: The new test passes. The existing `TestWorkspaceStore_List_SortedAlphabetically` will fail because all its workspaces have zero `LastUsedAt`, which still sorts alphabetically — verify this is still passing. If it does, great. If any existing tests fail due to sort order assumptions with non-zero times, fix them.

**Step 6: Commit**

```
feat(workspace): add LastUsedAt field with MRU sorting
```

---

### Task 2: Add `Update()` to WorkspaceStore

**Files:**
- Modify: `internal/workspace/workspacestore.go`
- Modify: `internal/workspace/workspacestore_test.go`

**Step 1: Write the failing test**

Add to `internal/workspace/workspacestore_test.go`:

```go
func TestWorkspaceStore_Update(t *testing.T) {
	store := setupWorkspaceStore(t)

	ws := &Workspace{ID: "ws-1", Name: "test", Path: "/path"}
	store.Add(ws)

	now := time.Now()
	ws.LastUsedAt = now
	err := store.Update(ws)
	require.NoError(t, err)

	retrieved, err := store.GetByID("ws-1")
	require.NoError(t, err)
	require.Equal(t, now.Unix(), retrieved.LastUsedAt.Unix())
}

func TestWorkspaceStore_Update_NotFound(t *testing.T) {
	store := setupWorkspaceStore(t)

	ws := &Workspace{ID: "nonexistent", Name: "test", Path: "/path"}
	err := store.Update(ws)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestWorkspaceStore_Update_Nil(t *testing.T) {
	store := setupWorkspaceStore(t)
	err := store.Update(nil)
	require.Error(t, err)
}

func TestWorkspaceStore_Update_EmptyID(t *testing.T) {
	store := setupWorkspaceStore(t)
	err := store.Update(&Workspace{Name: "test"})
	require.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `Update` method doesn't exist

**Step 3: Implement `Update()`**

Add to `internal/workspace/workspacestore.go`:

```go
func (s *WorkspaceStore) Update(ws *Workspace) error {
	if ws == nil {
		return errors.New("workspace cannot be nil")
	}
	if ws.ID == "" {
		return errors.New("workspace ID cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.workspaces[ws.ID]; !exists {
		return &WorkspaceNotFoundError{WorkspaceID: ws.ID}
	}

	copy := *ws
	s.workspaces[ws.ID] = &copy
	return nil
}
```

**Step 4: Run tests**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```
feat(workspace): add Update method to workspace store
```

---

### Task 3: Add metadata persistence to WorkspaceStore

**Files:**
- Modify: `internal/workspace/workspacestore.go`
- Modify: `internal/workspace/workspacestore_test.go`

**Step 1: Write the failing test**

Add to `internal/workspace/workspacestore_test.go`:

```go
func TestWorkspaceStore_MetadataPersistence(t *testing.T) {
	store := setupWorkspaceStore(t)

	now := time.Now()
	ws := &Workspace{ID: "ws-1", Name: "test", Path: "/path", LastUsedAt: now}
	store.Add(ws)
	store.saveMeta()

	store2 := NewWorkspaceStore(store.fs, store.configDir)
	store2.Add(&Workspace{ID: "ws-1", Name: "test", Path: "/path"})
	store2.loadMeta()

	retrieved, err := store2.GetByID("ws-1")
	require.NoError(t, err)
	require.Equal(t, now.Unix(), retrieved.LastUsedAt.Unix())
}

func TestWorkspaceStore_OnAppStart_MergesMetadata(t *testing.T) {
	rootDir := t.TempDir()
	os.MkdirAll(filepath.Join(rootDir, "project-alpha"), 0755)

	store, _ := setupWorkspaceStoreWithConfig(t, []string{rootDir})

	ctx := context.Background()
	err := store.OnAppStart(ctx)
	require.NoError(t, err)

	workspaces := store.List()
	require.Len(t, workspaces, 1)
	wsID := workspaces[0].ID

	now := time.Now()
	workspaces[0].LastUsedAt = now
	store.Update(&workspaces[0])
	store.saveMeta()

	store2 := NewWorkspaceStore(store.fs, store.configDir)
	err = store2.OnAppStart(ctx)
	require.NoError(t, err)

	retrieved, err := store2.GetByID(wsID)
	require.NoError(t, err)
	require.Equal(t, now.Unix(), retrieved.LastUsedAt.Unix())
}
```

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `saveMeta`/`loadMeta` don't exist

**Step 3: Implement metadata persistence**

Add to `internal/workspace/workspacestore.go`:

```go
type workspaceMeta struct {
	LastUsedAt time.Time `json:"last_used_at"`
}

func (s *WorkspaceStore) metaPath() string {
	return filepath.Join(s.configDir, "workspace_metadata.json")
}

func (s *WorkspaceStore) loadMeta() {
	data, err := afero.ReadFile(s.fs, s.metaPath())
	if err != nil {
		return
	}

	var meta map[string]workspaceMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return
	}

	for id, m := range meta {
		if ws, ok := s.workspaces[id]; ok {
			ws.LastUsedAt = m.LastUsedAt
		}
	}
}

func (s *WorkspaceStore) saveMeta() {
	meta := make(map[string]workspaceMeta)
	for id, ws := range s.workspaces {
		if !ws.LastUsedAt.IsZero() {
			meta[id] = workspaceMeta{LastUsedAt: ws.LastUsedAt}
		}
	}

	s.fs.MkdirAll(filepath.Dir(s.metaPath()), 0755)
	data, err := json.Marshal(meta)
	if err != nil {
		return
	}
	afero.WriteFile(s.fs, s.metaPath(), data, 0644)
}
```

**Step 4: Call `saveMeta()` from `Update()` and `loadMeta()` from `OnAppStart()`**

In `Update()`, add `s.saveMeta()` after updating the workspace (inside the lock, after the assignment).

In `OnAppStart()`, after the loop that calls `s.Add(ws)`, add `s.loadMeta()`.

**Step 5: Run tests**

Run: `task test`
Expected: PASS

**Step 6: Commit**

```
feat(workspace): add metadata persistence for LastUsedAt
```

---

### Task 4: Add `Touch()` to WorkspaceService

**Files:**
- Modify: `internal/workspace/workspaceservice.go`
- Modify: `internal/workspace/workspaceservice_test.go`

**Step 1: Write the failing test**

Add to `internal/workspace/workspaceservice_test.go`:

```go
func TestWorkspaceService_Touch(t *testing.T) {
	service, store := setupWorkspaceService(t)

	ws := &Workspace{ID: "ws-1", Name: "test", Path: "/path"}
	store.Add(ws)

	before := time.Now()
	ctx := context.Background()
	err := service.Touch(ctx, "ws-1")
	require.NoError(t, err)

	retrieved, err := store.GetByID("ws-1")
	require.NoError(t, err)
	require.False(t, retrieved.LastUsedAt.IsZero())
	require.True(t, retrieved.LastUsedAt.After(before) || retrieved.LastUsedAt.Equal(before))
}

func TestWorkspaceService_Touch_NotFound(t *testing.T) {
	service, _ := setupWorkspaceService(t)

	ctx := context.Background()
	err := service.Touch(ctx, "nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}
```

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `Touch` method doesn't exist

**Step 3: Implement `Touch()`**

Add to `internal/workspace/workspaceservice.go`:

```go
func (s *WorkspaceService) Touch(ctx context.Context, id string) error {
	ws, err := s.store.GetByID(id)
	if err != nil {
		return err
	}

	ws.LastUsedAt = time.Now()
	return s.store.Update(ws)
}
```

Add `"time"` to imports.

**Step 4: Run tests**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```
feat(workspace): add Touch method to workspace service
```

---

### Task 5: Switch SessionService from WorkspaceStore to WorkspaceService

**Files:**
- Modify: `internal/session/sessionservice.go:14-28`
- Modify: `internal/session/sessionmodule.go:21`
- Modify: `internal/session/sessionservice_test.go:18-31`

**Step 1: Update `SessionService` struct and constructor**

In `internal/session/sessionservice.go`, change `workspaceStore *workspace.WorkspaceStore` to `workspaceService *workspace.WorkspaceService`:

```go
type SessionService struct {
	store            *SessionStore
	workspaceService *workspace.WorkspaceService
	gitService       *git.GitService
	eventBus         eventbus.EventBus
}

func NewSessionService(store *SessionStore, workspaceService *workspace.WorkspaceService, gitService *git.GitService, bus eventbus.EventBus) *SessionService {
	return &SessionService{
		store:            store,
		workspaceService: workspaceService,
		gitService:       gitService,
		eventBus:         bus,
	}
}
```

**Step 2: Update all `s.workspaceStore` references to `s.workspaceService`**

In `resolveWorkspaceName`: change `s.workspaceStore.GetByID(session.WorkspaceID)` → `s.workspaceService.GetWorkspace(context.Background(), session.WorkspaceID)`

In `ListSessionsByWorkspace`: change `s.workspaceStore.GetByID(workspaceID)` → `s.workspaceService.GetWorkspace(ctx, workspaceID)`

In `CreateSession`: change `s.workspaceStore.GetByID(session.WorkspaceID)` → `s.workspaceService.GetWorkspace(ctx, session.WorkspaceID)`

In `UpdateSession`: change `s.workspaceStore.GetByID(session.WorkspaceID)` → `s.workspaceService.GetWorkspace(ctx, session.WorkspaceID)`

**Step 3: Update module wiring**

In `internal/session/sessionmodule.go` line 21, change:
```go
service := NewSessionService(store, workspaceModule.Store, workspaceModule.GitService, bus)
```
to:
```go
service := NewSessionService(store, workspaceModule.Service, workspaceModule.GitService, bus)
```

**Step 4: Update test setup**

In `internal/session/sessionservice_test.go`, update `setupSessionService`:

```go
func setupSessionService(t *testing.T) (*SessionService, *SessionStore, *workspace.WorkspaceStore) {
	t.Helper()

	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(afero.NewMemMapFs(), "/config")
	workspaceStore := workspace.NewWorkspaceStore(afero.NewMemMapFs(), "/config")

	workspaceStore.Add(&workspace.Workspace{ID: "ws-1", Name: "utena", Path: "/tmp/utena"})
	workspaceStore.Add(&workspace.Workspace{ID: "ws-2", Name: "other", Path: "/tmp/other"})

	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	gitService := git.NewGitService()
	service := NewSessionService(sessionStore, workspaceService, gitService, bus)
	return service, sessionStore, workspaceStore
}
```

Update all tests that create `SessionService` directly (the worktree tests) to also go through `WorkspaceService`:

In `TestSessionService_CreateSession_WithWorktree`, `TestSessionService_CreateSession_WithWorktree_InvalidBranch`, and `TestSessionService_CreateSession_NonGitWorkspace_SkipsWorktree`:

Replace `NewSessionService(sessionStore, workspaceStore, gitService, bus)` with:
```go
workspaceService := workspace.NewWorkspaceService(workspaceStore)
service := NewSessionService(sessionStore, workspaceService, gitService, bus)
```

**Step 5: Run tests**

Run: `task test`
Expected: PASS — all existing tests still pass

**Step 6: Commit**

```
refactor(session): use WorkspaceService instead of WorkspaceStore
```

---

### Task 6: Call `Touch()` on session create and activate

**Files:**
- Modify: `internal/session/sessionservice.go`
- Modify: `internal/session/sessionservice_test.go`

**Step 1: Write the failing tests**

Add to `internal/session/sessionservice_test.go`:

```go
func TestSessionService_CreateSession_TouchesWorkspace(t *testing.T) {
	service, _, workspaceStore := setupSessionService(t)

	session := &Session{
		ID:          "session-1",
		WorkspaceID: "ws-1",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session)
	require.NoError(t, err)

	ws, err := workspaceStore.GetByID("ws-1")
	require.NoError(t, err)
	require.False(t, ws.LastUsedAt.IsZero())
}

func TestSessionService_ActivateSession_TouchesWorkspace(t *testing.T) {
	service, sessionStore, workspaceStore := setupSessionService(t)

	session := &Session{
		ID:          "session-1",
		WorkspaceID: "ws-1",
		LastUsedAt:  time.Now().Add(-1 * time.Hour),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	_, err := service.ActivateSession(ctx, "session-1")
	require.NoError(t, err)

	ws, err := workspaceStore.GetByID("ws-1")
	require.NoError(t, err)
	require.False(t, ws.LastUsedAt.IsZero())
}
```

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — workspace `LastUsedAt` is still zero

**Step 3: Add Touch calls**

In `internal/session/sessionservice.go`:

In `CreateSession`, after `s.store.Add(session)` succeeds, add:

```go
if session.WorkspaceID != "" {
	s.workspaceService.Touch(ctx, session.WorkspaceID)
}
```

In `ActivateSession`, after `s.store.Update(session)` succeeds, add:

```go
if session.WorkspaceID != "" {
	s.workspaceService.Touch(ctx, session.ID)
}
```

Wait — `ActivateSession` takes `name` not ID. The session is already fetched. Use `session.WorkspaceID`:

```go
if session.WorkspaceID != "" {
	s.workspaceService.Touch(ctx, session.WorkspaceID)
}
```

**Step 4: Run tests**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```
feat(session): touch workspace on session create and activate
```

---

### Task 7: Final integration verification

**Step 1: Run full test suite**

Run: `task test`
Expected: All tests pass

**Step 2: Build**

Run: `task fmt`
Expected: Clean

**Step 3: Commit if any formatting changes**

Only if `task fmt` changed files.
