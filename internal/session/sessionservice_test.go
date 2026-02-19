package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
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

	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	gitService := git.NewGitService()
	service := NewSessionService(sessionStore, workspaceService, gitService, bus)
	return service, sessionStore, workspaceStore
}

func TestNewSessionService(t *testing.T) {
	service, _, _ := setupSessionService(t)
	require.NotNil(t, service)
	require.NotNil(t, service.store)
	require.NotNil(t, service.workspaceService)
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

	retrieved, err := sessionStore.GetByID("session-1")
	require.NoError(t, err)
	require.True(t, retrieved.IsDeleted)
	require.NotNil(t, retrieved.Cleanup)
}

func TestSessionService_DeleteSession_NotFound(t *testing.T) {
	service, _, _ := setupSessionService(t)

	ctx := context.Background()
	err := service.DeleteSession(ctx, "nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v failed: %s", args, string(out))
	}
	return dir
}

func TestSessionService_CreateSession_WithWorktree(t *testing.T) {
	repoPath := initTestRepo(t)

	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(afero.NewMemMapFs(), "/config")
	workspaceStore := workspace.NewWorkspaceStore(afero.NewMemMapFs(), "/config")
	workspaceStore.Add(&workspace.Workspace{ID: "ws-git", Name: "git-repo", Path: repoPath, IsGitRepo: true})

	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	gitService := git.NewGitService()
	service := NewSessionService(sessionStore, workspaceService, gitService, bus)

	session := &Session{
		ID:          "my-feature",
		WorkspaceID: "ws-git",
		BaseBranch:  "main",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session)
	require.NoError(t, err)

	expectedPath := filepath.Join(repoPath, ".worktrees", "my-feature")
	require.Equal(t, expectedPath, session.WorktreePath)

	info, err := os.Stat(expectedPath)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	retrieved, err := sessionStore.GetByID("my-feature")
	require.NoError(t, err)
	require.Equal(t, expectedPath, retrieved.WorktreePath)
	require.Equal(t, "main", retrieved.BaseBranch)
}

func TestSessionService_CreateSession_WithWorktree_InvalidBranch(t *testing.T) {
	repoPath := initTestRepo(t)

	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(afero.NewMemMapFs(), "/config")
	workspaceStore := workspace.NewWorkspaceStore(afero.NewMemMapFs(), "/config")
	workspaceStore.Add(&workspace.Workspace{ID: "ws-git", Name: "git-repo", Path: repoPath, IsGitRepo: true})

	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	gitService := git.NewGitService()
	service := NewSessionService(sessionStore, workspaceService, gitService, bus)

	session := &Session{
		ID:          "my-feature",
		WorkspaceID: "ws-git",
		BaseBranch:  "nonexistent",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to create worktree")

	_, err = sessionStore.GetByID("my-feature")
	require.Error(t, err)
}

func TestSessionService_CreateSession_NoBranch_SkipsWorktree(t *testing.T) {
	service, sessionStore, _ := setupSessionService(t)

	session := &Session{
		ID:          "my-session",
		WorkspaceID: "ws-1",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session)
	require.NoError(t, err)
	require.Empty(t, session.WorktreePath)

	retrieved, err := sessionStore.GetByID("my-session")
	require.NoError(t, err)
	require.Empty(t, retrieved.WorktreePath)
}

func TestSessionService_CreateSession_NonGitWorkspace_SkipsWorktree(t *testing.T) {
	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(afero.NewMemMapFs(), "/config")
	workspaceStore := workspace.NewWorkspaceStore(afero.NewMemMapFs(), "/config")
	workspaceStore.Add(&workspace.Workspace{ID: "ws-nogit", Name: "plain", Path: "/tmp/plain", IsGitRepo: false})

	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	gitService := git.NewGitService()
	service := NewSessionService(sessionStore, workspaceService, gitService, bus)

	session := &Session{
		ID:          "my-session",
		WorkspaceID: "ws-nogit",
		BaseBranch:  "main",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session)
	require.NoError(t, err)
	require.Empty(t, session.WorktreePath)
}

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
