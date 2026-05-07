package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionService_OnAppStart_BackfillsLegacySessionWorkspace(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)
	ctx := context.Background()

	legacy := &Session{
		Name:        "legacy-session",
		WorkspaceID: ws1ID,
		Status:      StatusActive,
		LastUsedAt:  time.Now(),
	}
	require.NoError(t, sessionStore.Add(legacy))

	require.NoError(t, service.OnAppStart(ctx))

	loaded, err := sessionStore.GetByID(legacy.ID)
	require.NoError(t, err)
	require.Len(t, loaded.Workspaces, 1, "backfill should add one junction row")
	require.Equal(t, ws1ID, loaded.Workspaces[0].WorkspaceID)
	require.Equal(t, 0, loaded.Workspaces[0].Position)
	require.NotEmpty(t, loaded.SessionRoot, "backfill should set SessionRoot")

	require.NoError(t, service.OnAppStart(ctx))
	again, err := sessionStore.GetByID(legacy.ID)
	require.NoError(t, err)
	require.Len(t, again.Workspaces, 1, "second backfill must remain idempotent")
}

func TestSessionService_CreateMultiSession_RejectsSingleWorkspace(t *testing.T) {
	service, _, _, _, ws1ID, _ := setupSessionService(t)
	ctx := context.Background()

	_, err := service.CreateMultiSession(ctx, CreateMultiSessionInput{
		Name:         "only-one",
		WorkspaceIDs: []uint{ws1ID},
		Branch:       "feat/something",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least 2 workspaces")
}

func TestSessionService_CreateMultiSession_RejectsNonGitWorkspaces(t *testing.T) {
	service, _, _, _, ws1ID, ws2ID := setupSessionService(t)
	ctx := context.Background()

	_, err := service.CreateMultiSession(ctx, CreateMultiSessionInput{
		Name:         "mixed",
		WorkspaceIDs: []uint{ws1ID, ws2ID},
		Branch:       "feat/x",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "git repository")
}

func TestSessionService_CreateMultiSession_RejectsDuplicateWorkspaceIDs(t *testing.T) {
	service, _, _, _, ws1ID, _ := setupSessionService(t)
	ctx := context.Background()

	_, err := service.CreateMultiSession(ctx, CreateMultiSessionInput{
		Name:         "dups",
		WorkspaceIDs: []uint{ws1ID, ws1ID},
		Branch:       "feat/x",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "more than once")
}

func TestSessionService_CreateMultiSession_RejectsExistingSessionRootDir(t *testing.T) {
	service, _, workspaceStore, _, ws1ID, ws2ID := setupSessionService(t)
	ctx := context.Background()

	ws1, err := workspaceStore.GetByID(ws1ID)
	require.NoError(t, err)
	ws1.IsGitRepo = true
	require.NoError(t, workspaceStore.Update(ws1))
	ws2, err := workspaceStore.GetByID(ws2ID)
	require.NoError(t, err)
	ws2.IsGitRepo = true
	require.NoError(t, workspaceStore.Update(ws2))

	dir := filepath.Join(service.sessionsRoot, "blocker")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	_, err = service.CreateMultiSession(ctx, CreateMultiSessionInput{
		Name:         "blocker",
		WorkspaceIDs: []uint{ws1ID, ws2ID},
		Branch:       "feat/x",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists on disk")
}

func TestSessionService_isSessionGitHealthy_NoBranches(t *testing.T) {
	service, _, _, _, _, _ := setupSessionService(t)
	ctx := context.Background()

	sess := &Session{Workspaces: []SessionWorkspace{
		{WorkspaceID: 1, Workspace: nil, GitBranch: nil},
	}}
	require.True(t, service.isSessionGitHealthy(ctx, sess), "slots without branches must not block health")
}

func TestSessionService_RepairSession_RoutesMultiToRepairPath(t *testing.T) {
	service, sessionStore, _, _, ws1ID, ws2ID := setupSessionService(t)
	ctx := context.Background()
	swStore := NewSessionWorkspaceStore(service.store.db)

	sessionRoot := filepath.Join(service.sessionsRoot, "repair-multi")
	sess := &Session{
		Name:        "repair-multi",
		WorkspaceID: ws1ID,
		Status:      StatusBroken,
		StatusError: "something",
		SessionRoot: sessionRoot,
		LastUsedAt:  time.Now(),
	}
	require.NoError(t, sessionStore.Add(sess))
	require.NoError(t, swStore.Add(&SessionWorkspace{SessionID: sess.ID, WorkspaceID: ws1ID, Position: 0, WorktreePath: filepath.Join(sessionRoot, "a")}))
	require.NoError(t, swStore.Add(&SessionWorkspace{SessionID: sess.ID, WorkspaceID: ws2ID, Position: 1, WorktreePath: filepath.Join(sessionRoot, "b")}))

	updated, err := service.RepairSession(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, StatusCreating, updated.Status, "RepairSession must transition the session to creating before kicking off async repair")

	require.Eventually(t, func() bool {
		final, err := sessionStore.GetByID(sess.ID)
		if err != nil {
			return false
		}
		return final.Status != StatusCreating
	}, 3*time.Second, 50*time.Millisecond)

	if _, err := os.Stat(sessionRoot); err != nil {
		require.Failf(t, "session root not created", "expected %q to exist after repair: %v", sessionRoot, err)
	}
}
