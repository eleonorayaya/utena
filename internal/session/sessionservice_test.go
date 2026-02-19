package session

import (
	"context"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupSessionService(t *testing.T) (*SessionService, *SessionStore, *workspace.WorkspaceStore) {
	t.Helper()

	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(afero.NewMemMapFs(), "/config")
	workspaceStore := workspace.NewWorkspaceStore(afero.NewMemMapFs(), "/config")

	workspaceStore.Add(&workspace.Workspace{ID: "ws-1", Name: "utena", Path: "/tmp/utena"})
	workspaceStore.Add(&workspace.Workspace{ID: "ws-2", Name: "other", Path: "/tmp/other"})

	service := NewSessionService(sessionStore, workspaceStore, bus)
	return service, sessionStore, workspaceStore
}

func TestNewSessionService(t *testing.T) {
	service, _, _ := setupSessionService(t)
	require.NotNil(t, service)
	require.NotNil(t, service.store)
	require.NotNil(t, service.workspaceStore)
}

func TestSessionService_OnAppStart(t *testing.T) {
	service, _, _ := setupSessionService(t)
	ctx := context.Background()
	err := service.OnAppStart(ctx)
	require.NoError(t, err)
}

func TestSessionService_OnAppEnd(t *testing.T) {
	service, _, _ := setupSessionService(t)
	ctx := context.Background()
	err := service.OnAppEnd(ctx)
	require.NoError(t, err)
}

func TestSessionService_ListSessions(t *testing.T) {
	service, sessionStore, _ := setupSessionService(t)

	// Add test sessions
	now := time.Now()
	session1 := &Session{ID: "session-1", WorkspaceID: "ws-1", LastUsedAt: now.Add(-1 * time.Hour)}
	session2 := &Session{ID: "session-2", WorkspaceID: "ws-2", LastUsedAt: now}
	sessionStore.Add(session1)
	sessionStore.Add(session2)

	ctx := context.Background()
	sessions, err := service.ListSessions(ctx)
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	// Verify MRU sorting
	require.Equal(t, "session-2", sessions[0].ID, "Most recent session should be first")
}

func TestSessionService_ListSessionsByWorkspace(t *testing.T) {
	service, sessionStore, _ := setupSessionService(t)

	// Add test sessions
	now := time.Now()
	session1 := &Session{ID: "session-1", WorkspaceID: "ws-1", LastUsedAt: now.Add(-1 * time.Hour)}
	session2 := &Session{ID: "session-2", WorkspaceID: "ws-2", LastUsedAt: now}
	session3 := &Session{ID: "session-3", WorkspaceID: "ws-1", LastUsedAt: now}
	sessionStore.Add(session1)
	sessionStore.Add(session2)
	sessionStore.Add(session3)

	ctx := context.Background()
	sessions, err := service.ListSessionsByWorkspace(ctx, "ws-1")
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	// Verify only ws-1 sessions returned
	for _, session := range sessions {
		require.Equal(t, "ws-1", session.WorkspaceID)
	}

	// Verify MRU sorting
	require.Equal(t, "session-3", sessions[0].ID, "Most recent ws-1 session should be first")
}

func TestSessionService_ListSessionsByWorkspace_InvalidWorkspace(t *testing.T) {
	service, _, _ := setupSessionService(t)

	ctx := context.Background()
	_, err := service.ListSessionsByWorkspace(ctx, "nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionService_GetSession(t *testing.T) {
	service, sessionStore, _ := setupSessionService(t)

	session := &Session{
		ID:          "session-1",
		WorkspaceID: "ws-1",
		IsAttached:  true,
		IsActive:    true,
		LastUsedAt:  time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	retrieved, err := service.GetSession(ctx, "session-1")
	require.NoError(t, err)
	require.Equal(t, session.ID, retrieved.ID)
	require.Equal(t, session.WorkspaceID, retrieved.WorkspaceID)
	require.Equal(t, session.IsAttached, retrieved.IsAttached)
}

func TestSessionService_GetSession_NotFound(t *testing.T) {
	service, _, _ := setupSessionService(t)

	ctx := context.Background()
	_, err := service.GetSession(ctx, "nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionService_CreateSession(t *testing.T) {
	service, sessionStore, _ := setupSessionService(t)

	session := &Session{
		ID:          "session-1",
		WorkspaceID: "ws-1",
		IsAttached:  true,
		IsActive:    true,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session)
	require.NoError(t, err)

	// Verify session was created
	retrieved, err := sessionStore.GetByID("session-1")
	require.NoError(t, err)
	require.Equal(t, session.ID, retrieved.ID)

	// Verify LastUsedAt was set
	require.False(t, retrieved.LastUsedAt.IsZero())
}

func TestSessionService_CreateSession_InvalidWorkspace(t *testing.T) {
	service, _, _ := setupSessionService(t)

	session := &Session{
		ID:          "session-1",
		WorkspaceID: "nonexistent",
		LastUsedAt:  time.Now(),
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionService_UpdateSession(t *testing.T) {
	service, sessionStore, _ := setupSessionService(t)

	session := &Session{
		ID:          "session-1",
		WorkspaceID: "ws-1",
		IsAttached:  false,
		IsActive:    true,
		LastUsedAt:  time.Now(),
	}
	sessionStore.Add(session)

	// Update session
	session.IsAttached = true
	ctx := context.Background()
	err := service.UpdateSession(ctx, session)
	require.NoError(t, err)

	// Verify update
	retrieved, err := sessionStore.GetByID("session-1")
	require.NoError(t, err)
	require.True(t, retrieved.IsAttached)
}

func TestSessionService_UpdateSession_InvalidWorkspace(t *testing.T) {
	service, sessionStore, _ := setupSessionService(t)

	session := &Session{
		ID:          "session-1",
		WorkspaceID: "ws-1",
		LastUsedAt:  time.Now(),
	}
	sessionStore.Add(session)

	// Try to update with invalid workspace
	session.WorkspaceID = "nonexistent"
	ctx := context.Background()
	err := service.UpdateSession(ctx, session)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionService_DeleteSession(t *testing.T) {
	service, sessionStore, _ := setupSessionService(t)

	session := &Session{
		ID:          "session-1",
		WorkspaceID: "ws-1",
		LastUsedAt:  time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	err := service.DeleteSession(ctx, "session-1")
	require.NoError(t, err)

	// Verify deletion
	_, err = sessionStore.GetByID("session-1")
	require.Error(t, err)
}

func TestSessionService_DeleteSession_NotFound(t *testing.T) {
	service, _, _ := setupSessionService(t)

	ctx := context.Background()
	err := service.DeleteSession(ctx, "nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}
