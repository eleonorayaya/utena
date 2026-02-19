package zellij

import (
	"context"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupZellijService(t *testing.T) (*ZellijService, *session.SessionService, *session.SessionStore) {
	t.Helper()

	ctx := context.Background()

	bus := eventbus.NewEventBus()
	sessionStore := session.NewSessionStore(afero.NewMemMapFs(), "/config")
	workspaceStore := workspace.NewWorkspaceStore(afero.NewMemMapFs(), "/config")

	workspaceStore.Add(&workspace.Workspace{ID: "ws-1", Name: "utena", Path: "/tmp/utena"})

	sessionService := session.NewSessionService(sessionStore, workspaceStore, bus)
	err := sessionService.OnAppStart(ctx)
	require.NoError(t, err)

	zellijService := NewZellijService(sessionService, bus)
	err = zellijService.OnAppStart(ctx)
	require.NoError(t, err)

	return zellijService, sessionService, sessionStore
}

func TestZellijService_ProcessSessionUpdate_CreateNewSessions(t *testing.T) {
	service, _, sessionStore := setupZellijService(t)
	ctx := context.Background()

	req := &UpdateSessionsRequest{
		Sessions: []SessionUpdate{
			{
				Name:             "session-1",
				ConnectedClients: 1,
			},
			{
				Name:             "session-2",
				ConnectedClients: 0,
			},
		},
	}

	err := service.ProcessSessionUpdate(ctx, req)
	require.NoError(t, err)

	sessions := sessionStore.List()
	require.Len(t, sessions, 2)

	session1, err := sessionStore.GetByID("session-1")
	require.NoError(t, err)
	require.Equal(t, "session-1", session1.ID)
	require.True(t, session1.IsAttached)
	require.True(t, session1.IsActive)
	require.False(t, session1.IsDead)
	require.Empty(t, session1.WorkspaceID)

	session2, err := sessionStore.GetByID("session-2")
	require.NoError(t, err)
	require.Equal(t, "session-2", session2.ID)
	require.False(t, session2.IsAttached)
	require.True(t, session2.IsActive)
	require.False(t, session2.IsDead)
}

func TestZellijService_ProcessSessionUpdate_UpdateExistingSessions(t *testing.T) {
	service, _, sessionStore := setupZellijService(t)
	ctx := context.Background()

	oldTime := time.Now().Add(-1 * time.Hour)
	existingSession := &session.Session{
		ID:          "session-1",
		WorkspaceID: "ws-1",
		IsAttached:  false,
		IsActive:    false,
		IsDead:      false,
		LastUsedAt:  oldTime,
	}
	sessionStore.Add(existingSession)

	req := &UpdateSessionsRequest{
		Sessions: []SessionUpdate{
			{
				Name:             "session-1",
				ConnectedClients: 1,
			},
		},
	}

	time.Sleep(10 * time.Millisecond)

	err := service.ProcessSessionUpdate(ctx, req)
	require.NoError(t, err)

	updatedSession, err := sessionStore.GetByID("session-1")
	require.NoError(t, err)
	require.True(t, updatedSession.IsAttached)
	require.True(t, updatedSession.IsActive)
	require.False(t, updatedSession.IsDead)
	require.True(t, updatedSession.LastUsedAt.After(oldTime))
}

func TestZellijService_ProcessSessionUpdate_MixedCreateAndUpdate(t *testing.T) {
	service, _, sessionStore := setupZellijService(t)
	ctx := context.Background()

	existingSession := &session.Session{
		ID:          "existing-session",
		WorkspaceID: "ws-1",
		IsAttached:  false,
		IsActive:    false,
		IsDead:      false,
		LastUsedAt:  time.Now().Add(-1 * time.Hour),
	}
	sessionStore.Add(existingSession)

	req := &UpdateSessionsRequest{
		Sessions: []SessionUpdate{
			{
				Name:             "existing-session",
				ConnectedClients: 1,
			},
			{
				Name:             "new-session",
				ConnectedClients: 0,
			},
		},
	}

	err := service.ProcessSessionUpdate(ctx, req)
	require.NoError(t, err)

	sessions := sessionStore.List()
	require.Len(t, sessions, 2)

	updated, err := sessionStore.GetByID("existing-session")
	require.NoError(t, err)
	require.True(t, updated.IsAttached)
	require.True(t, updated.IsActive)
	require.False(t, updated.IsDead)

	new, err := sessionStore.GetByID("new-session")
	require.NoError(t, err)
	require.False(t, new.IsAttached)
	require.True(t, new.IsActive)
	require.False(t, new.IsDead)
}

func TestZellijService_ProcessSessionUpdate_MarkDeadSessions(t *testing.T) {
	service, _, sessionStore := setupZellijService(t)
	ctx := context.Background()

	sess1 := &session.Session{
		ID:          "session-1",
		WorkspaceID: "ws-1",
		IsActive:    true,
		IsDead:      false,
		LastUsedAt:  time.Now(),
	}
	sess2 := &session.Session{
		ID:          "session-2",
		WorkspaceID: "ws-1",
		IsActive:    true,
		IsDead:      false,
		LastUsedAt:  time.Now(),
	}
	sess3 := &session.Session{
		ID:          "session-3",
		WorkspaceID: "ws-1",
		IsActive:    true,
		IsDead:      false,
		LastUsedAt:  time.Now(),
	}
	sessionStore.Add(sess1)
	sessionStore.Add(sess2)
	sessionStore.Add(sess3)

	req := &UpdateSessionsRequest{
		Sessions: []SessionUpdate{
			{
				Name:             "session-1",
				ConnectedClients: 1,
			},
			{
				Name:             "session-3",
				ConnectedClients: 0,
			},
		},
	}

	err := service.ProcessSessionUpdate(ctx, req)
	require.NoError(t, err)

	updated1, err := sessionStore.GetByID("session-1")
	require.NoError(t, err)
	require.False(t, updated1.IsDead)
	require.True(t, updated1.IsActive)

	updated2, err := sessionStore.GetByID("session-2")
	require.NoError(t, err)
	require.True(t, updated2.IsDead)

	updated3, err := sessionStore.GetByID("session-3")
	require.NoError(t, err)
	require.False(t, updated3.IsDead)
	require.True(t, updated3.IsActive)
}

func TestZellijService_ProcessSessionUpdate_AllSessionsDead(t *testing.T) {
	service, _, sessionStore := setupZellijService(t)
	ctx := context.Background()

	sess1 := &session.Session{
		ID:          "session-1",
		WorkspaceID: "ws-1",
		IsActive:    true,
		IsDead:      false,
		LastUsedAt:  time.Now(),
	}
	sessionStore.Add(sess1)

	req := &UpdateSessionsRequest{
		Sessions: []SessionUpdate{},
	}

	err := service.ProcessSessionUpdate(ctx, req)
	require.NoError(t, err)

	updated, err := sessionStore.GetByID("session-1")
	require.NoError(t, err)
	require.True(t, updated.IsDead)
}
