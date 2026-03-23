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
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) db.Database {
	t.Helper()
	database, err := db.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	database.Migrate(&workspace.Workspace{}, &Session{}, &claude.ClaudeSession{})
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
		TmuxSessionName: "session-1",
		WorkspaceID:     ws1ID,
		IsAttached:      true,
		Status:          StatusReady,
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
	session := &Session{TmuxSessionName: "session-1", WorkspaceID: ws1ID, LastUsedAt: time.Now()}
	err := store.Add(session)
	require.NoError(t, err)
	require.NotZero(t, session.ID)
}

func TestSessionStore_Add_Duplicate(t *testing.T) {
	store, ws1ID, ws2ID := setupSessionStore(t)

	session1 := &Session{TmuxSessionName: "session-1", WorkspaceID: ws1ID, LastUsedAt: time.Now()}
	session2 := &Session{TmuxSessionName: "session-1", WorkspaceID: ws2ID, LastUsedAt: time.Now()}

	err := store.Add(session1)
	require.NoError(t, err)

	err = store.Add(session2)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrSessionAlreadyExists))
	require.Contains(t, err.Error(), "'session-1' already exists")
}

func TestSessionStore_GetByID(t *testing.T) {
	store, ws1ID, _ := setupSessionStore(t)

	session := &Session{
		TmuxSessionName: "session-1",
		WorkspaceID:     ws1ID,
		IsAttached:      false,
		Status:          StatusReady,
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
	session1 := &Session{TmuxSessionName: "session-1", WorkspaceID: ws1ID, LastUsedAt: now.Add(-2 * time.Hour)}
	session2 := &Session{TmuxSessionName: "session-2", WorkspaceID: ws2ID, LastUsedAt: now}
	session3 := &Session{TmuxSessionName: "session-3", WorkspaceID: ws1ID, LastUsedAt: now.Add(-1 * time.Hour)}

	store.Add(session1)
	store.Add(session2)
	store.Add(session3)

	list := store.List()
	require.Len(t, list, 3)

	require.Equal(t, "session-2", list[0].TmuxSessionName, "Most recent session should be first")
	require.Equal(t, "session-3", list[1].TmuxSessionName, "Second most recent session should be second")
	require.Equal(t, "session-1", list[2].TmuxSessionName, "Oldest session should be last")
}

func TestSessionStore_List_Empty(t *testing.T) {
	store, _, _ := setupSessionStore(t)
	list := store.List()
	require.Empty(t, list)
}

func TestSessionStore_ListByWorkspace(t *testing.T) {
	store, ws1ID, ws2ID := setupSessionStore(t)

	now := time.Now()
	session1 := &Session{TmuxSessionName: "session-1", WorkspaceID: ws1ID, LastUsedAt: now.Add(-2 * time.Hour)}
	session2 := &Session{TmuxSessionName: "session-2", WorkspaceID: ws2ID, LastUsedAt: now}
	session3 := &Session{TmuxSessionName: "session-3", WorkspaceID: ws1ID, LastUsedAt: now.Add(-1 * time.Hour)}

	store.Add(session1)
	store.Add(session2)
	store.Add(session3)

	ws1Sessions := store.ListByWorkspace(ws1ID)
	require.Len(t, ws1Sessions, 2)

	for _, session := range ws1Sessions {
		require.Equal(t, ws1ID, session.WorkspaceID)
	}

	require.Equal(t, "session-3", ws1Sessions[0].TmuxSessionName, "Most recent ws-1 session should be first")
	require.Equal(t, "session-1", ws1Sessions[1].TmuxSessionName, "Older ws-1 session should be second")

	ws2Sessions := store.ListByWorkspace(ws2ID)
	require.Len(t, ws2Sessions, 1)
	require.Equal(t, "session-2", ws2Sessions[0].TmuxSessionName)
}

func TestSessionStore_ListByWorkspace_Empty(t *testing.T) {
	store, _, _ := setupSessionStore(t)
	list := store.ListByWorkspace(99999)
	require.Empty(t, list)
}

func TestSessionStore_Update(t *testing.T) {
	store, ws1ID, _ := setupSessionStore(t)

	session := &Session{
		TmuxSessionName: "session-1",
		WorkspaceID:     ws1ID,
		IsAttached:      false,
		Status:          StatusReady,
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

	session := &Session{TmuxSessionName: "nonexistent", WorkspaceID: ws1ID, LastUsedAt: time.Now()}
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

	session := &Session{TmuxSessionName: "session-1", WorkspaceID: ws1ID, LastUsedAt: time.Now()}
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
				TmuxSessionName: fmt.Sprintf("session-%d", id),
				WorkspaceID:     ws1ID,
				LastUsedAt:      time.Now(),
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
