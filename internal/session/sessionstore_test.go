package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

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
	database.Migrate(&workspace.Workspace{}, &Session{})
	t.Cleanup(func() { database.Close() })
	return database
}

func setupSessionStore(t *testing.T) *SessionStore {
	t.Helper()
	database := setupTestDB(t)
	database.Create(&workspace.Workspace{ID: "ws-1", Name: "utena", Path: "/tmp/utena"})
	database.Create(&workspace.Workspace{ID: "ws-2", Name: "other", Path: "/tmp/other"})
	return NewSessionStore(database)
}

func TestNewSessionStore(t *testing.T) {
	store := setupSessionStore(t)
	require.NotNil(t, store)
	list := store.List()
	require.Empty(t, list)
}

func TestSessionStore_Add(t *testing.T) {
	store := setupSessionStore(t)

	session := &Session{
		ID:              "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     "ws-1",
		IsAttached:      true,
		Status:          StatusReady,
		LastUsedAt:      time.Now(),
	}

	err := store.Add(session)
	require.NoError(t, err)

	retrieved, err := store.GetByID("session-1")
	require.NoError(t, err)
	require.Equal(t, session.ID, retrieved.ID)
	require.Equal(t, session.WorkspaceID, retrieved.WorkspaceID)
}

func TestSessionStore_Add_NilSession(t *testing.T) {
	store := setupSessionStore(t)
	err := store.Add(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
}

func TestSessionStore_Add_EmptyID(t *testing.T) {
	store := setupSessionStore(t)
	session := &Session{WorkspaceID: "ws-1", LastUsedAt: time.Now()}
	err := store.Add(session)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ID cannot be empty")
}

func TestSessionStore_Add_WithWorkspaceID(t *testing.T) {
	store := setupSessionStore(t)
	session := &Session{ID: "session-1", TmuxSessionName: "session-1", WorkspaceID: "ws-1", LastUsedAt: time.Now()}
	err := store.Add(session)
	require.NoError(t, err)
}

func TestSessionStore_Add_Duplicate(t *testing.T) {
	store := setupSessionStore(t)

	session1 := &Session{ID: "session-1", TmuxSessionName: "session-1", WorkspaceID: "ws-1", LastUsedAt: time.Now()}
	session2 := &Session{ID: "session-1", TmuxSessionName: "session-1-dup", WorkspaceID: "ws-2", LastUsedAt: time.Now()}

	err := store.Add(session1)
	require.NoError(t, err)

	err = store.Add(session2)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrSessionAlreadyExists))
	require.Contains(t, err.Error(), "'session-1' already exists")
}

func TestSessionStore_GetByID(t *testing.T) {
	store := setupSessionStore(t)

	session := &Session{
		ID:              "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     "ws-1",
		IsAttached:      false,
		Status:          StatusReady,
		LastUsedAt:      time.Now(),
	}

	store.Add(session)

	retrieved, err := store.GetByID("session-1")
	require.NoError(t, err)
	require.Equal(t, session.ID, retrieved.ID)
	require.Equal(t, session.WorkspaceID, retrieved.WorkspaceID)
	require.Equal(t, session.IsAttached, retrieved.IsAttached)
	require.Equal(t, session.Status, retrieved.Status)
}

func TestSessionStore_GetByID_NotFound(t *testing.T) {
	store := setupSessionStore(t)

	_, err := store.GetByID("nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionStore_List(t *testing.T) {
	store := setupSessionStore(t)

	now := time.Now()
	session1 := &Session{ID: "session-1", TmuxSessionName: "session-1", WorkspaceID: "ws-1", LastUsedAt: now.Add(-2 * time.Hour)}
	session2 := &Session{ID: "session-2", TmuxSessionName: "session-2", WorkspaceID: "ws-2", LastUsedAt: now}
	session3 := &Session{ID: "session-3", TmuxSessionName: "session-3", WorkspaceID: "ws-1", LastUsedAt: now.Add(-1 * time.Hour)}

	store.Add(session1)
	store.Add(session2)
	store.Add(session3)

	list := store.List()
	require.Len(t, list, 3)

	require.Equal(t, "session-2", list[0].ID, "Most recent session should be first")
	require.Equal(t, "session-3", list[1].ID, "Second most recent session should be second")
	require.Equal(t, "session-1", list[2].ID, "Oldest session should be last")
}

func TestSessionStore_List_Empty(t *testing.T) {
	store := setupSessionStore(t)
	list := store.List()
	require.Empty(t, list)
}

func TestSessionStore_ListByWorkspace(t *testing.T) {
	store := setupSessionStore(t)

	now := time.Now()
	session1 := &Session{ID: "session-1", TmuxSessionName: "session-1", WorkspaceID: "ws-1", LastUsedAt: now.Add(-2 * time.Hour)}
	session2 := &Session{ID: "session-2", TmuxSessionName: "session-2", WorkspaceID: "ws-2", LastUsedAt: now}
	session3 := &Session{ID: "session-3", TmuxSessionName: "session-3", WorkspaceID: "ws-1", LastUsedAt: now.Add(-1 * time.Hour)}

	store.Add(session1)
	store.Add(session2)
	store.Add(session3)

	ws1Sessions := store.ListByWorkspace("ws-1")
	require.Len(t, ws1Sessions, 2)

	for _, session := range ws1Sessions {
		require.Equal(t, "ws-1", session.WorkspaceID)
	}

	require.Equal(t, "session-3", ws1Sessions[0].ID, "Most recent ws-1 session should be first")
	require.Equal(t, "session-1", ws1Sessions[1].ID, "Older ws-1 session should be second")

	ws2Sessions := store.ListByWorkspace("ws-2")
	require.Len(t, ws2Sessions, 1)
	require.Equal(t, "session-2", ws2Sessions[0].ID)
}

func TestSessionStore_ListByWorkspace_Empty(t *testing.T) {
	store := setupSessionStore(t)
	list := store.ListByWorkspace("nonexistent")
	require.Empty(t, list)
}

func TestSessionStore_Update(t *testing.T) {
	store := setupSessionStore(t)

	session := &Session{
		ID:              "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     "ws-1",
		IsAttached:      false,
		Status:          StatusReady,
		LastUsedAt:      time.Now(),
	}

	store.Add(session)

	session.IsAttached = true
	session.LastUsedAt = time.Now().Add(1 * time.Hour)

	err := store.Update(session)
	require.NoError(t, err)

	retrieved, err := store.GetByID("session-1")
	require.NoError(t, err)
	require.True(t, retrieved.IsAttached)
}

func TestSessionStore_Update_NotFound(t *testing.T) {
	store := setupSessionStore(t)

	session := &Session{ID: "nonexistent", TmuxSessionName: "nonexistent", WorkspaceID: "ws-1", LastUsedAt: time.Now()}
	err := store.Update(session)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionStore_Update_NilSession(t *testing.T) {
	store := setupSessionStore(t)
	err := store.Update(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
}

func TestSessionStore_Update_EmptyID(t *testing.T) {
	store := setupSessionStore(t)
	session := &Session{WorkspaceID: "ws-1", LastUsedAt: time.Now()}
	err := store.Update(session)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ID cannot be empty")
}

func TestSessionStore_Delete(t *testing.T) {
	store := setupSessionStore(t)

	session := &Session{ID: "session-1", TmuxSessionName: "session-1", WorkspaceID: "ws-1", LastUsedAt: time.Now()}
	store.Add(session)

	err := store.Delete("session-1")
	require.NoError(t, err)

	_, err = store.GetByID("session-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionStore_Delete_NotFound(t *testing.T) {
	store := setupSessionStore(t)

	err := store.Delete("nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionStore_Delete_EmptyID(t *testing.T) {
	store := setupSessionStore(t)

	err := store.Delete("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ID cannot be empty")
}

func TestSessionStore_ConcurrentAccess(t *testing.T) {
	store := setupSessionStore(t)

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			session := &Session{
				ID:              fmt.Sprintf("session-%d", id),
				TmuxSessionName: fmt.Sprintf("session-%d", id),
				WorkspaceID:     "ws-1",
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
	store := setupSessionStore(t)

	ctx := context.Background()
	err := store.OnAppStart(ctx)
	require.NoError(t, err)
}

func TestSessionStore_OnAppEnd(t *testing.T) {
	store := setupSessionStore(t)

	ctx := context.Background()
	err := store.OnAppEnd(ctx)
	require.NoError(t, err)
}
