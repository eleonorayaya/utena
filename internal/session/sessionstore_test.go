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
	"github.com/eleonorayaya/utena/internal/git"
	utmux "github.com/eleonorayaya/utena/internal/tmux"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) db.Database {
	t.Helper()
	database, err := db.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	database.Migrate(&workspace.Workspace{}, &git.Repo{}, &git.Branch{}, &git.Worktree{}, &git.PullRequest{}, &utmux.TmuxSession{}, &Session{}, &claude.ClaudeSession{})
	t.Cleanup(func() { database.Close() })
	return database
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

func TestNewSessionStore(t *testing.T) {
	store, _, _ := setupSessionStore(t)
	require.NotNil(t, store)
	list := store.List()
	require.Empty(t, list)
}

func TestSessionStore_Add(t *testing.T) {
	store, ws1ID, _ := setupSessionStore(t)

	session := &Session{
		Name:        "session-1",
		WorkspaceID:     ws1ID,
		IsAttached:      true,
		Status:          StatusActive,
		LastUsedAt:      time.Now(),
	}

	err := store.Add(session)
	require.NoError(t, err)
	require.NotZero(t, session.ID)

	retrieved, err := store.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, session.ID, retrieved.ID)
	require.Equal(t, session.WorkspaceID, retrieved.WorkspaceID)
}

func TestSessionStore_Add_NilSession(t *testing.T) {
	store, _, _ := setupSessionStore(t)
	err := store.Add(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
}

func TestSessionStore_Add_WithWorkspaceID(t *testing.T) {
	store, ws1ID, _ := setupSessionStore(t)
	session := &Session{Name: "session-1", WorkspaceID: ws1ID, LastUsedAt: time.Now()}
	err := store.Add(session)
	require.NoError(t, err)
	require.NotZero(t, session.ID)
}

func TestSessionStore_Add_DuplicateTmuxSessionID(t *testing.T) {
	database, wsID, _, ts := setupTestDBWithGitAndTmux(t)
	store := NewSessionStore(database)

	session1 := &Session{Name: "session-1", WorkspaceID: wsID, TmuxSessionID: &ts.ID, LastUsedAt: time.Now()}
	session2 := &Session{Name: "session-2", WorkspaceID: wsID, TmuxSessionID: &ts.ID, LastUsedAt: time.Now()}

	err := store.Add(session1)
	require.NoError(t, err)

	err = store.Add(session2)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrSessionAlreadyExists))
}

func TestSessionStore_GetByID(t *testing.T) {
	store, ws1ID, _ := setupSessionStore(t)

	session := &Session{
		Name:        "session-1",
		WorkspaceID:     ws1ID,
		IsAttached:      false,
		Status:          StatusActive,
		LastUsedAt:      time.Now(),
	}

	store.Add(session)

	retrieved, err := store.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, session.ID, retrieved.ID)
	require.Equal(t, session.WorkspaceID, retrieved.WorkspaceID)
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
	store, ws1ID, ws2ID := setupSessionStore(t)

	now := time.Now()
	session1 := &Session{Name: "session-1", WorkspaceID: ws1ID, LastUsedAt: now.Add(-2 * time.Hour)}
	session2 := &Session{Name: "session-2", WorkspaceID: ws2ID, LastUsedAt: now}
	session3 := &Session{Name: "session-3", WorkspaceID: ws1ID, LastUsedAt: now.Add(-1 * time.Hour)}

	store.Add(session1)
	store.Add(session2)
	store.Add(session3)

	list := store.List()
	require.Len(t, list, 3)

	require.Equal(t, "session-2", list[0].Name, "Most recent session should be first")
	require.Equal(t, "session-3", list[1].Name, "Second most recent session should be second")
	require.Equal(t, "session-1", list[2].Name, "Oldest session should be last")
}

func TestSessionStore_List_Empty(t *testing.T) {
	store, _, _ := setupSessionStore(t)
	list := store.List()
	require.Empty(t, list)
}

func TestSessionStore_ListByWorkspace(t *testing.T) {
	store, ws1ID, ws2ID := setupSessionStore(t)

	now := time.Now()
	session1 := &Session{Name: "session-1", WorkspaceID: ws1ID, LastUsedAt: now.Add(-2 * time.Hour)}
	session2 := &Session{Name: "session-2", WorkspaceID: ws2ID, LastUsedAt: now}
	session3 := &Session{Name: "session-3", WorkspaceID: ws1ID, LastUsedAt: now.Add(-1 * time.Hour)}

	store.Add(session1)
	store.Add(session2)
	store.Add(session3)

	ws1Sessions := store.ListByWorkspace(ws1ID)
	require.Len(t, ws1Sessions, 2)

	for _, session := range ws1Sessions {
		require.Equal(t, ws1ID, session.WorkspaceID)
	}

	require.Equal(t, "session-3", ws1Sessions[0].Name, "Most recent ws-1 session should be first")
	require.Equal(t, "session-1", ws1Sessions[1].Name, "Older ws-1 session should be second")

	ws2Sessions := store.ListByWorkspace(ws2ID)
	require.Len(t, ws2Sessions, 1)
	require.Equal(t, "session-2", ws2Sessions[0].Name)
}

func TestSessionStore_ListByWorkspace_Empty(t *testing.T) {
	store, _, _ := setupSessionStore(t)
	list := store.ListByWorkspace(99999)
	require.Empty(t, list)
}

func TestSessionStore_Update(t *testing.T) {
	store, ws1ID, _ := setupSessionStore(t)

	session := &Session{
		Name:        "session-1",
		WorkspaceID:     ws1ID,
		IsAttached:      false,
		Status:          StatusActive,
		LastUsedAt:      time.Now(),
	}

	store.Add(session)

	session.IsAttached = true
	session.LastUsedAt = time.Now().Add(1 * time.Hour)

	err := store.Update(session)
	require.NoError(t, err)

	retrieved, err := store.GetByID(session.ID)
	require.NoError(t, err)
	require.True(t, retrieved.IsAttached)
}

func TestSessionStore_Update_NotFound(t *testing.T) {
	store, ws1ID, _ := setupSessionStore(t)

	session := &Session{Name: "nonexistent", WorkspaceID: ws1ID, LastUsedAt: time.Now()}
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
	store, ws1ID, _ := setupSessionStore(t)
	session := &Session{WorkspaceID: ws1ID, LastUsedAt: time.Now()}
	err := store.Update(session)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ID cannot be zero")
}

func TestSessionStore_Delete(t *testing.T) {
	store, ws1ID, _ := setupSessionStore(t)

	session := &Session{Name: "session-1", WorkspaceID: ws1ID, LastUsedAt: time.Now()}
	store.Add(session)

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
	store, ws1ID, _ := setupSessionStore(t)

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			session := &Session{
				Name:        fmt.Sprintf("session-%d", id),
				WorkspaceID: ws1ID,
				LastUsedAt:  time.Now(),
			}
			store.Add(session)
		}(i)
	}

	wg.Wait()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.List()
		}()
	}

	wg.Wait()

	list := store.List()
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
	database, err := db.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	database.Migrate(&workspace.Workspace{}, &git.Repo{}, &git.Branch{}, &git.Worktree{}, &git.PullRequest{}, &utmux.TmuxSession{}, &Session{}, &claude.ClaudeSession{})
	t.Cleanup(func() { database.Close() })

	ws := &workspace.Workspace{Name: "utena", Path: "/tmp/utena"}
	database.Create(ws)

	repo := &git.Repo{Path: "/tmp/utena", FullName: "eleonorayaya/utena"}
	database.Create(repo)

	branch := &git.Branch{Name: "feature-x", RepoID: repo.ID, ExistsLocal: true}
	database.Create(branch)

	ts := &utmux.TmuxSession{Name: "utena-feature-x", StartDir: "/tmp/utena", IsAlive: true}
	database.Create(ts)

	return database, ws.ID, branch, ts
}

func TestSessionStore_GetByID_LoadsGitBranchAndTmuxSession(t *testing.T) {
	database, wsID, branch, ts := setupTestDBWithGitAndTmux(t)
	store := NewSessionStore(database)

	session := &Session{
		Name:        "utena-feature-x",
		WorkspaceID:     wsID,
		BranchID:        &branch.ID,
		TmuxSessionID:   &ts.ID,
		Status:          StatusActive,
		LastUsedAt:      time.Now(),
	}
	require.NoError(t, store.Add(session))

	retrieved, err := store.GetByID(session.ID)
	require.NoError(t, err)
	require.NotNil(t, retrieved.GitBranch)
	require.Equal(t, "feature-x", retrieved.GitBranch.Name)
	require.NotNil(t, retrieved.TmuxSession)
	require.Equal(t, "utena-feature-x", retrieved.TmuxSession.Name)
	require.True(t, retrieved.TmuxSession.IsAlive)
}

func TestSessionStore_GetByBranchID(t *testing.T) {
	database, wsID, branch, ts := setupTestDBWithGitAndTmux(t)
	store := NewSessionStore(database)

	session := &Session{
		Name:        "utena-feature-x",
		WorkspaceID:     wsID,
		BranchID:        &branch.ID,
		TmuxSessionID:   &ts.ID,
		Status:          StatusActive,
		LastUsedAt:      time.Now(),
	}
	require.NoError(t, store.Add(session))

	retrieved, err := store.GetByBranchID(branch.ID)
	require.NoError(t, err)
	require.Equal(t, session.ID, retrieved.ID)
	require.NotNil(t, retrieved.GitBranch)
	require.Equal(t, "feature-x", retrieved.GitBranch.Name)
}

func TestSessionStore_GetByBranchID_NotFound(t *testing.T) {
	store, _, _ := setupSessionStore(t)
	_, err := store.GetByBranchID(99999)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionStore_GetByTmuxSessionID(t *testing.T) {
	database, wsID, branch, ts := setupTestDBWithGitAndTmux(t)
	store := NewSessionStore(database)

	session := &Session{
		Name:        "utena-feature-x",
		WorkspaceID:     wsID,
		BranchID:        &branch.ID,
		TmuxSessionID:   &ts.ID,
		Status:          StatusActive,
		LastUsedAt:      time.Now(),
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
	store, wsID, _ := setupSessionStore(t)

	session := &Session{
		Name:        "no-fk-session",
		WorkspaceID:     wsID,
		Status:          StatusActive,
		LastUsedAt:      time.Now(),
	}
	require.NoError(t, store.Add(session))

	retrieved, err := store.GetByID(session.ID)
	require.NoError(t, err)
	require.Nil(t, retrieved.BranchID)
	require.Nil(t, retrieved.TmuxSessionID)
	require.Nil(t, retrieved.GitBranch)
	require.Zero(t, retrieved.TmuxSession.ID)
}

func TestSessionStore_List_LoadsNewRelationships(t *testing.T) {
	database, wsID, branch, ts := setupTestDBWithGitAndTmux(t)
	store := NewSessionStore(database)

	session1 := &Session{
		Name:        "utena-feature-x",
		WorkspaceID:     wsID,
		BranchID:        &branch.ID,
		TmuxSessionID:   &ts.ID,
		Status:          StatusActive,
		LastUsedAt:      time.Now(),
	}
	session2 := &Session{
		Name:        "utena-no-fk",
		WorkspaceID:     wsID,
		Status:          StatusActive,
		LastUsedAt:      time.Now().Add(-1 * time.Hour),
	}
	require.NoError(t, store.Add(session1))
	require.NoError(t, store.Add(session2))

	list := store.List()
	require.Len(t, list, 2)

	require.NotNil(t, list[0].GitBranch)
	require.Equal(t, "feature-x", list[0].GitBranch.Name)
	require.NotNil(t, list[0].TmuxSession)
	require.Equal(t, "utena-feature-x", list[0].TmuxSession.Name)

	require.Nil(t, list[1].GitBranch)
	require.Zero(t, list[1].TmuxSession.ID)
}

func TestSessionStore_StatusError(t *testing.T) {
	store, wsID, _ := setupSessionStore(t)

	session := &Session{
		Name:        "broken-session",
		WorkspaceID:     wsID,
		Status:          StatusBroken,
		StatusError:     "worktree creation failed",
		LastUsedAt:      time.Now(),
	}
	require.NoError(t, store.Add(session))

	retrieved, err := store.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusBroken, retrieved.Status)
	require.Equal(t, "worktree creation failed", retrieved.StatusError)
}
