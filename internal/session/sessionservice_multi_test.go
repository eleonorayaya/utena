package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionService_OnAppStart_MarksOrphanedSessionBroken(t *testing.T) {
	service, sessionStore, _, _, _, _ := setupSessionService(t)
	ctx := context.Background()

	orphan := &Session{
		Name:       "orphan-session",
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}
	require.NoError(t, sessionStore.Add(orphan))

	require.NoError(t, service.OnAppStart(ctx))

	loaded, err := sessionStore.GetByID(orphan.ID)
	require.NoError(t, err)
	require.Equal(t, StatusBroken, loaded.Status)
	require.Contains(t, loaded.StatusError, "predates")
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

func TestSessionService_isSessionGitHealthy_NoWorktrees(t *testing.T) {
	service, _, _, _, _, _ := setupSessionService(t)
	ctx := context.Background()

	sess := &Session{}
	require.True(t, service.isSessionGitHealthy(ctx, sess), "session without worktrees must not block health")
}

func TestSessionService_RepairSession_MultiTransitionsToCreating(t *testing.T) {
	service, sessionStore, _, _, ws1ID, ws2ID := setupSessionService(t)
	ctx := context.Background()
	swtStore := NewSessionWorktreeStore(service.store.db)

	sessionRoot := filepath.Join(service.sessionsRoot, "repair-multi")
	sess := &Session{
		Name:        "repair-multi",
		Status:      StatusBroken,
		StatusError: "something",
		SessionRoot: sessionRoot,
		LastUsedAt:  time.Now(),
	}
	require.NoError(t, sessionStore.Add(sess))
	attachTestWorktree(t, service.store.db, swtStore, sess.ID, ws1ID, 0)
	attachTestWorktree(t, service.store.db, swtStore, sess.ID, ws2ID, 1)

	tmuxRecord, err := service.tmuxService.RegisterPending(SanitizeTmuxName(sess.Name), sessionRoot, nil)
	require.NoError(t, err)
	sess.TmuxSessionID = &tmuxRecord.ID
	require.NoError(t, sessionStore.Update(sess))

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
}

func TestSessionService_RepairSession_RejectsSessionWithoutTmuxRecord(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)
	ctx := context.Background()
	swtStore := NewSessionWorktreeStore(service.store.db)

	sess := &Session{
		Name:        "no-tmux",
		Status:      StatusBroken,
		StatusError: "something",
		LastUsedAt:  time.Now(),
	}
	require.NoError(t, sessionStore.Add(sess))
	attachTestWorktree(t, service.store.db, swtStore, sess.ID, ws1ID, 0)

	_, err := service.RepairSession(ctx, sess.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "tmux record")
}
