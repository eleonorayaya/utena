package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionWorkspaceStore_Add_AndList(t *testing.T) {
	sessionStore, ws1ID, ws2ID := setupSessionStore(t)
	swStore := NewSessionWorkspaceStore(sessionStore.db)

	sess := &Session{Name: "multi-1", WorkspaceID: ws1ID, Status: StatusCreating, LastUsedAt: time.Now()}
	require.NoError(t, sessionStore.Add(sess))

	for i, wsID := range []uint{ws1ID, ws2ID} {
		row := &SessionWorkspace{
			SessionID:    sess.ID,
			WorkspaceID:  wsID,
			WorktreePath: "/tmp/multi-1/ws",
			Position:     i,
		}
		require.NoError(t, swStore.Add(row))
	}

	rows, err := swStore.ListBySessionID(sess.ID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, ws1ID, rows[0].WorkspaceID)
	require.Equal(t, 0, rows[0].Position)
	require.Equal(t, ws2ID, rows[1].WorkspaceID)
	require.Equal(t, 1, rows[1].Position)
}

func TestSessionWorkspaceStore_DuplicateRejected(t *testing.T) {
	sessionStore, ws1ID, _ := setupSessionStore(t)
	swStore := NewSessionWorkspaceStore(sessionStore.db)

	sess := &Session{Name: "dup", WorkspaceID: ws1ID, Status: StatusCreating, LastUsedAt: time.Now()}
	require.NoError(t, sessionStore.Add(sess))

	require.NoError(t, swStore.Add(&SessionWorkspace{SessionID: sess.ID, WorkspaceID: ws1ID}))
	err := swStore.Add(&SessionWorkspace{SessionID: sess.ID, WorkspaceID: ws1ID})
	require.Error(t, err)
}

func TestSessionStore_Loaded_PreloadsWorkspaces(t *testing.T) {
	sessionStore, ws1ID, ws2ID := setupSessionStore(t)
	swStore := NewSessionWorkspaceStore(sessionStore.db)

	sess := &Session{Name: "loaded", WorkspaceID: ws1ID, Status: StatusActive, SessionRoot: "/tmp/loaded", LastUsedAt: time.Now()}
	require.NoError(t, sessionStore.Add(sess))
	require.NoError(t, swStore.Add(&SessionWorkspace{SessionID: sess.ID, WorkspaceID: ws1ID, Position: 0, WorktreePath: "/tmp/loaded/a"}))
	require.NoError(t, swStore.Add(&SessionWorkspace{SessionID: sess.ID, WorkspaceID: ws2ID, Position: 1, WorktreePath: "/tmp/loaded/b"}))

	loaded, err := sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.Len(t, loaded.Workspaces, 2)
	require.Equal(t, "/tmp/loaded", loaded.SessionRoot)
	require.True(t, loaded.IsMulti())
	require.NotNil(t, loaded.PrimaryWorkspace())
	require.Equal(t, ws1ID, loaded.PrimaryWorkspace().WorkspaceID)
}
