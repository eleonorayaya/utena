package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/eleonorayaya/utena/internal/git"
	utmux "github.com/eleonorayaya/utena/internal/tmux"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) db.Database {
	return testdb.New(t, &workspace.Workspace{}, &git.Repo{}, &git.Branch{}, &git.Worktree{}, &git.PullRequest{}, &utmux.TmuxSession{}, &Session{}, &SessionWorktree{}, &claude.ClaudeSession{}, &SessionAction{})
}

func setupSessionStore(t *testing.T) (*SessionStore, uint, uint) {
	t.Helper()
	database := setupTestDB(t)
	ws1 := &workspace.Workspace{Name: "utena", Path: "/tmp/utena"}
	ws2 := &workspace.Workspace{Name: "other", Path: "/tmp/other"}
	database.Create(ws1)
	database.Create(ws2)
	return NewSessionStore(database), ws1.ID, ws2.ID
}

// addTestSession creates a session linked to the given workspace via a fresh
// Worktree + SessionWorktree junction row. The worktree is recorded in
// `missing` status to keep health checks short-circuit without needing real
// on-disk git infrastructure. Tests that exercise live git should attach
// a worktree pointing at a real path themselves.
func addTestSession(t *testing.T, store *SessionStore, swtStore *SessionWorktreeStore, sess *Session, wsID uint) {
	t.Helper()
	require.NoError(t, store.Add(sess))
	attachTestWorktree(t, store.db, swtStore, sess.ID, wsID, 0)
}

func attachTestWorktree(t *testing.T, database db.Database, swtStore *SessionWorktreeStore, sessionID uint, wsID uint, position int) *git.Worktree {
	t.Helper()
	ws := &workspace.Workspace{}
	require.NoError(t, database.First(ws, "id = ?", wsID).Error)

	var repoID uint
	if ws.RepoID != nil && *ws.RepoID != 0 {
		repoID = *ws.RepoID
	} else {
		repo := &git.Repo{Path: ws.Path, FullName: fmt.Sprintf("test/%s-%d", ws.Name, sessionID)}
		require.NoError(t, database.Create(repo).Error)
		repoID = repo.ID
		ws.RepoID = &repoID
		require.NoError(t, database.Save(ws).Error)
	}

	branch := &git.Branch{Name: fmt.Sprintf("test-branch-%d-%d", sessionID, position), RepoID: repoID, ExistsLocal: true}
	require.NoError(t, database.Create(branch).Error)

	wt := &git.Worktree{
		Path:        fmt.Sprintf("/tmp/utena-test-worktree-%d-%d", sessionID, position),
		BranchID:    branch.ID,
		RepoID:      repoID,
		WorkspaceID: &wsID,
		Status:      git.WorktreeStatusPending,
	}
	require.NoError(t, database.Create(wt).Error)

	require.NoError(t, swtStore.Add(&SessionWorktree{SessionID: sessionID, WorktreeID: wt.ID, Position: position}))
	return wt
}

// attachExistingWorktree links a session to a workspace via an existing branch
// (e.g. one already created by the test).
func attachExistingWorktree(t *testing.T, database db.Database, swtStore *SessionWorktreeStore, sessionID uint, wsID uint, branch *git.Branch, worktreePath string, position int) *git.Worktree {
	t.Helper()
	ws := &workspace.Workspace{}
	require.NoError(t, database.First(ws, "id = ?", wsID).Error)
	repoID := branch.RepoID
	if repoID == 0 && ws.RepoID != nil {
		repoID = *ws.RepoID
	}
	wt := &git.Worktree{
		Path:        worktreePath,
		BranchID:    branch.ID,
		RepoID:      repoID,
		WorkspaceID: &wsID,
		Status:      git.WorktreeStatusPresent,
	}
	require.NoError(t, database.Create(wt).Error)
	require.NoError(t, swtStore.Add(&SessionWorktree{SessionID: sessionID, WorktreeID: wt.ID, Position: position}))
	return wt
}

func TestNewSessionStore(t *testing.T) {
	store, _, _ := setupSessionStore(t)
	require.NotNil(t, store)
	list, _ := store.List()
	require.Empty(t, list)
}

func TestSessionStore_Add(t *testing.T) {
	store, _, _ := setupSessionStore(t)

	session := &Session{
		Name:       "session-1",
		IsAttached: true,
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}

	err := store.Add(session)
	require.NoError(t, err)
	require.NotZero(t, session.ID)

	retrieved, err := store.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, session.ID, retrieved.ID)
	require.Equal(t, session.IsAttached, retrieved.IsAttached)
}

func TestSessionStore_Add_NilSession(t *testing.T) {
	store, _, _ := setupSessionStore(t)
	err := store.Add(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
}

func TestSessionStore_Add_DuplicateTmuxSessionID(t *testing.T) {
	database, _, _, ts := setupTestDBWithGitAndTmux(t)
	store := NewSessionStore(database)

	session1 := &Session{Name: "session-1", TmuxSessionID: &ts.ID, LastUsedAt: time.Now()}
	session2 := &Session{Name: "session-2", TmuxSessionID: &ts.ID, LastUsedAt: time.Now()}

	err := store.Add(session1)
	require.NoError(t, err)

	err = store.Add(session2)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrSessionAlreadyExists))
}

func TestSessionStore_GetByID(t *testing.T) {
	store, _, _ := setupSessionStore(t)

	session := &Session{
		Name:       "session-1",
		IsAttached: false,
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}

	require.NoError(t, store.Add(session))

	retrieved, err := store.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, session.ID, retrieved.ID)
	require.Equal(t, session.IsAttached, retrieved.IsAttached)
	require.Equal(t, session.Status, retrieved.Status)
}

func TestSessionStore_GetByID_NotFound(t *testing.T) {
	store, _, _ := setupSessionStore(t)

	_, err := store.GetByID(99999)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionStore_List(t *testing.T) {
	store, _, _ := setupSessionStore(t)

	now := time.Now()
	session1 := &Session{Name: "session-1", LastUsedAt: now.Add(-2 * time.Hour)}
	session2 := &Session{Name: "session-2", LastUsedAt: now}
	session3 := &Session{Name: "session-3", LastUsedAt: now.Add(-1 * time.Hour)}

	require.NoError(t, store.Add(session1))
	require.NoError(t, store.Add(session2))
	require.NoError(t, store.Add(session3))

	list, _ := store.List()
	require.Len(t, list, 3)

	require.Equal(t, "session-2", list[0].Name, "Most recent session should be first")
	require.Equal(t, "session-3", list[1].Name, "Second most recent session should be second")
	require.Equal(t, "session-1", list[2].Name, "Oldest session should be last")
}

func TestSessionStore_List_Empty(t *testing.T) {
	store, _, _ := setupSessionStore(t)
	list, _ := store.List()
	require.Empty(t, list)
}

func TestSessionStore_ListByWorkspace(t *testing.T) {
	store, ws1ID, ws2ID := setupSessionStore(t)
	swtStore := NewSessionWorktreeStore(store.db)

	now := time.Now()
	session1 := &Session{Name: "session-1", LastUsedAt: now.Add(-2 * time.Hour)}
	session2 := &Session{Name: "session-2", LastUsedAt: now}
	session3 := &Session{Name: "session-3", LastUsedAt: now.Add(-1 * time.Hour)}

	addTestSession(t, store, swtStore, session1, ws1ID)
	addTestSession(t, store, swtStore, session2, ws2ID)
	addTestSession(t, store, swtStore, session3, ws1ID)

	ws1Sessions, err := store.ListByWorkspace(ws1ID)
	require.NoError(t, err)
	require.Len(t, ws1Sessions, 2)

	require.Equal(t, "session-3", ws1Sessions[0].Name, "Most recent ws-1 session should be first")
	require.Equal(t, "session-1", ws1Sessions[1].Name, "Older ws-1 session should be second")

	ws2Sessions, err := store.ListByWorkspace(ws2ID)
	require.NoError(t, err)
	require.Len(t, ws2Sessions, 1)
	require.Equal(t, "session-2", ws2Sessions[0].Name)
}

func TestSessionStore_ListByWorkspace_Empty(t *testing.T) {
	store, _, _ := setupSessionStore(t)
	list, _ := store.ListByWorkspace(99999)
	require.Empty(t, list)
}

func TestSessionStore_Update(t *testing.T) {
	store, _, _ := setupSessionStore(t)

	session := &Session{
		Name:       "session-1",
		IsAttached: false,
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}

	require.NoError(t, store.Add(session))

	session.IsAttached = true
	session.LastUsedAt = time.Now().Add(1 * time.Hour)

	err := store.Update(session)
	require.NoError(t, err)

	retrieved, err := store.GetByID(session.ID)
	require.NoError(t, err)
	require.True(t, retrieved.IsAttached)
}

func TestSessionStore_Update_NotFound(t *testing.T) {
	store, _, _ := setupSessionStore(t)

	session := &Session{Name: "nonexistent", LastUsedAt: time.Now()}
	session.ID = 99999
	err := store.Update(session)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionStore_Update_NilSession(t *testing.T) {
	store, _, _ := setupSessionStore(t)
	err := store.Update(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
}

func TestSessionStore_Update_ZeroID(t *testing.T) {
	store, _, _ := setupSessionStore(t)
	session := &Session{LastUsedAt: time.Now()}
	err := store.Update(session)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ID cannot be zero")
}

func TestSessionStore_Delete(t *testing.T) {
	store, _, _ := setupSessionStore(t)

	session := &Session{Name: "session-1", LastUsedAt: time.Now()}
	require.NoError(t, store.Add(session))

	err := store.Delete(session.ID)
	require.NoError(t, err)

	_, err = store.GetByID(session.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionStore_Delete_NotFound(t *testing.T) {
	store, _, _ := setupSessionStore(t)

	err := store.Delete(99999)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionStore_Delete_ZeroID(t *testing.T) {
	store, _, _ := setupSessionStore(t)

	err := store.Delete(0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ID cannot be zero")
}

func TestSessionStore_ConcurrentAccess(t *testing.T) {
	store, _, _ := setupSessionStore(t)

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			session := &Session{
				Name:       fmt.Sprintf("session-%d", id),
				LastUsedAt: time.Now(),
			}
			require.NoError(t, store.Add(session))
		}(i)
	}

	wg.Wait()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = store.List()
		}()
	}

	wg.Wait()

	list, _ := store.List()
	require.Len(t, list, numGoroutines)
}

func TestSessionStore_OnAppStart(t *testing.T) {
	store, _, _ := setupSessionStore(t)

	ctx := context.Background()
	err := store.OnAppStart(ctx)
	require.NoError(t, err)
}

func TestSessionStore_OnAppEnd(t *testing.T) {
	store, _, _ := setupSessionStore(t)

	ctx := context.Background()
	err := store.OnAppEnd(ctx)
	require.NoError(t, err)
}

func setupTestDBWithGitAndTmux(t *testing.T) (db.Database, uint, *git.Branch, *utmux.TmuxSession) {
	t.Helper()
	database := testdb.New(t, &workspace.Workspace{}, &git.Repo{}, &git.Branch{}, &git.Worktree{}, &git.PullRequest{}, &utmux.TmuxSession{}, &Session{}, &SessionWorktree{}, &claude.ClaudeSession{}, &SessionAction{})

	repo := &git.Repo{Path: "/tmp/utena", FullName: "eleonorayaya/utena"}
	database.Create(repo)
	repoID := repo.ID

	ws := &workspace.Workspace{Name: "utena", Path: "/tmp/utena", RepoID: &repoID}
	database.Create(ws)

	branch := &git.Branch{Name: "feature-x", RepoID: repo.ID, ExistsLocal: true}
	database.Create(branch)

	ts := &utmux.TmuxSession{Name: "utena-feature-x", StartDir: "/tmp/utena", Status: utmux.TmuxStatusActive}
	database.Create(ts)

	return database, ws.ID, branch, ts
}

func TestSessionStore_GetByID_LoadsWorktreeBranchAndTmuxSession(t *testing.T) {
	database, wsID, branch, ts := setupTestDBWithGitAndTmux(t)
	store := NewSessionStore(database)
	swtStore := NewSessionWorktreeStore(database)

	session := &Session{
		Name:          "utena-feature-x",
		TmuxSessionID: &ts.ID,
		Status:        StatusActive,
		LastUsedAt:    time.Now(),
	}
	require.NoError(t, store.Add(session))
	attachExistingWorktree(t, database, swtStore, session.ID, wsID, branch, "/tmp/utena/worktree-1", 0)

	retrieved, err := store.GetByID(session.ID)
	require.NoError(t, err)
	require.Len(t, retrieved.Worktrees, 1)
	require.NotNil(t, retrieved.Worktrees[0].Worktree)
	require.NotNil(t, retrieved.Worktrees[0].Worktree.Branch)
	require.Equal(t, "feature-x", retrieved.Worktrees[0].Worktree.Branch.Name)
	require.NotNil(t, retrieved.TmuxSession)
	require.Equal(t, "utena-feature-x", retrieved.TmuxSession.Name)
	require.Equal(t, utmux.TmuxStatusActive, retrieved.TmuxSession.Status)
}

func TestSessionStore_GetByBranchID(t *testing.T) {
	database, wsID, branch, ts := setupTestDBWithGitAndTmux(t)
	store := NewSessionStore(database)
	swtStore := NewSessionWorktreeStore(database)

	session := &Session{
		Name:          "utena-feature-x",
		TmuxSessionID: &ts.ID,
		Status:        StatusActive,
		LastUsedAt:    time.Now(),
	}
	require.NoError(t, store.Add(session))
	attachExistingWorktree(t, database, swtStore, session.ID, wsID, branch, "/tmp/utena/worktree-by-branch", 0)

	retrieved, err := store.GetByBranchID(branch.ID)
	require.NoError(t, err)
	require.Equal(t, session.ID, retrieved.ID)
	require.Len(t, retrieved.Worktrees, 1)
	require.NotNil(t, retrieved.Worktrees[0].Worktree.Branch)
	require.Equal(t, "feature-x", retrieved.Worktrees[0].Worktree.Branch.Name)
}

func TestSessionStore_GetByBranchID_NotFound(t *testing.T) {
	store, _, _ := setupSessionStore(t)
	_, err := store.GetByBranchID(99999)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionStore_GetByTmuxSessionID(t *testing.T) {
	database, _, _, ts := setupTestDBWithGitAndTmux(t)
	store := NewSessionStore(database)

	session := &Session{
		Name:          "utena-feature-x",
		TmuxSessionID: &ts.ID,
		Status:        StatusActive,
		LastUsedAt:    time.Now(),
	}
	require.NoError(t, store.Add(session))

	retrieved, err := store.GetByTmuxSessionID(ts.ID)
	require.NoError(t, err)
	require.Equal(t, session.ID, retrieved.ID)
	require.NotNil(t, retrieved.TmuxSession)
	require.Equal(t, "utena-feature-x", retrieved.TmuxSession.Name)
}

func TestSessionStore_GetByTmuxSessionID_NotFound(t *testing.T) {
	store, _, _ := setupSessionStore(t)
	_, err := store.GetByTmuxSessionID(99999)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionStore_NullableForeignKeys(t *testing.T) {
	store, _, _ := setupSessionStore(t)

	session := &Session{
		Name:       "no-fk-session",
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}
	require.NoError(t, store.Add(session))

	retrieved, err := store.GetByID(session.ID)
	require.NoError(t, err)
	require.Nil(t, retrieved.TmuxSessionID)
	require.Empty(t, retrieved.Worktrees)
	require.Zero(t, retrieved.TmuxSession.ID)
}

func TestSessionStore_List_LoadsNewRelationships(t *testing.T) {
	database, wsID, branch, ts := setupTestDBWithGitAndTmux(t)
	store := NewSessionStore(database)
	swtStore := NewSessionWorktreeStore(database)

	session1 := &Session{
		Name:          "utena-feature-x",
		TmuxSessionID: &ts.ID,
		Status:        StatusActive,
		LastUsedAt:    time.Now(),
	}
	session2 := &Session{
		Name:       "utena-no-fk",
		Status:     StatusActive,
		LastUsedAt: time.Now().Add(-1 * time.Hour),
	}
	require.NoError(t, store.Add(session1))
	attachExistingWorktree(t, database, swtStore, session1.ID, wsID, branch, "/tmp/utena/list-relations", 0)
	require.NoError(t, store.Add(session2))

	list, _ := store.List()
	require.Len(t, list, 2)

	require.Len(t, list[0].Worktrees, 1)
	require.NotNil(t, list[0].Worktrees[0].Worktree.Branch)
	require.Equal(t, "feature-x", list[0].Worktrees[0].Worktree.Branch.Name)
	require.NotNil(t, list[0].TmuxSession)
	require.Equal(t, "utena-feature-x", list[0].TmuxSession.Name)

	require.Empty(t, list[1].Worktrees)
	require.Zero(t, list[1].TmuxSession.ID)
}

func TestSessionStore_StatusError(t *testing.T) {
	store, _, _ := setupSessionStore(t)

	session := &Session{
		Name:        "broken-session",
		Status:      StatusBroken,
		StatusError: "worktree creation failed",
		LastUsedAt:  time.Now(),
	}
	require.NoError(t, store.Add(session))

	retrieved, err := store.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusBroken, retrieved.Status)
	require.Equal(t, "worktree creation failed", retrieved.StatusError)
}
