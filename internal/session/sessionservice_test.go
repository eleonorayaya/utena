package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	utmux "github.com/eleonorayaya/utena/internal/tmux"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func createTmuxService(t *testing.T, database db.Database, mock *utmux.MockRunner, bus eventbus.EventBus) *utmux.TmuxService {
	t.Helper()
	tmuxStore := utmux.NewTmuxStore(database)
	module := utmux.NewTmuxModuleWithRunner(mock, tmuxStore, bus)
	return module.Service
}

func setupSessionService(t *testing.T) (*SessionService, *SessionStore, *workspace.WorkspaceStore, *utmux.MockRunner, uint, uint) {
	t.Helper()

	database := setupTestDB(t)
	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")

	ws1 := &workspace.Workspace{Name: "utena", Path: "/tmp/utena"}
	ws2 := &workspace.Workspace{Name: "other", Path: "/tmp/other"}
	workspaceStore.Add(ws1)
	workspaceStore.Add(ws2)

	mock := utmux.NewMockRunner()
	tmuxService := createTmuxService(t, database, mock, bus)
	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	gitDB, err := db.OpenInMemory()
	require.NoError(t, err)
	gitDB.Migrate(&git.Repo{}, &git.Branch{}, &git.Worktree{}, &git.PullRequest{})
	t.Cleanup(func() { gitDB.Close() })
	gitService := git.NewGitService(gitDB)
	service := NewSessionService(sessionStore, workspaceService, gitService, tmuxService, bus, "eqt/", t.TempDir())
	return service, sessionStore, workspaceStore, mock, ws1.ID, ws2.ID
}

func waitForStatus(t *testing.T, store *SessionStore, id uint, status SessionStatus, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		sess, err := store.GetByID(id)
		if err == nil && sess.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	sess, _ := store.GetByID(id)
	t.Fatalf("session %d did not reach status %q within %v (current: %q)", id, status, timeout, sess.Status)
}

func TestNewSessionService(t *testing.T) {
	service, _, _, _, _, _ := setupSessionService(t)
	require.NotNil(t, service)
	require.NotNil(t, service.store)
	require.NotNil(t, service.workspaceService)
}

func TestSessionService_OnAppStart(t *testing.T) {
	service, _, _, _, _, _ := setupSessionService(t)
	ctx := context.Background()
	err := service.OnAppStart(ctx)
	require.NoError(t, err)
}

func TestSessionService_OnAppEnd(t *testing.T) {
	service, _, _, _, _, _ := setupSessionService(t)
	ctx := context.Background()
	err := service.OnAppEnd(ctx)
	require.NoError(t, err)
}

func TestSessionService_ListSessions(t *testing.T) {
	service, sessionStore, _, _, ws1ID, ws2ID := setupSessionService(t)

	now := time.Now()
	session1 := &Session{TmuxSessionName: "session-1", Name: "session-1", WorkspaceID: ws1ID, Status: StatusReady, LastUsedAt: now.Add(-1 * time.Hour)}
	session2 := &Session{TmuxSessionName: "session-2", Name: "session-2", WorkspaceID: ws2ID, Status: StatusReady, LastUsedAt: now}
	sessionStore.Add(session1)
	sessionStore.Add(session2)

	ctx := context.Background()
	sessions, err := service.ListSessions(ctx)
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	require.Equal(t, "session-2", sessions[0].TmuxSessionName, "Most recent session should be first")
}

func TestSessionService_ListSessionsByWorkspace(t *testing.T) {
	service, sessionStore, _, _, ws1ID, ws2ID := setupSessionService(t)

	now := time.Now()
	session1 := &Session{TmuxSessionName: "session-1", Name: "session-1", WorkspaceID: ws1ID, Status: StatusReady, LastUsedAt: now.Add(-1 * time.Hour)}
	session2 := &Session{TmuxSessionName: "session-2", Name: "session-2", WorkspaceID: ws2ID, Status: StatusReady, LastUsedAt: now}
	session3 := &Session{TmuxSessionName: "session-3", Name: "session-3", WorkspaceID: ws1ID, Status: StatusReady, LastUsedAt: now}
	sessionStore.Add(session1)
	sessionStore.Add(session2)
	sessionStore.Add(session3)

	ctx := context.Background()
	sessions, err := service.ListSessionsByWorkspace(ctx, ws1ID)
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	for _, session := range sessions {
		require.Equal(t, ws1ID, session.WorkspaceID)
	}

	require.Equal(t, "session-3", sessions[0].TmuxSessionName, "Most recent ws-1 session should be first")
}

func TestSessionService_ListSessionsByWorkspace_InvalidWorkspace(t *testing.T) {
	service, _, _, _, _, _ := setupSessionService(t)

	ctx := context.Background()
	_, err := service.ListSessionsByWorkspace(ctx, 99999)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionService_GetSession(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		TmuxSessionName: "session-1",
		Name:            "session-1",
		WorkspaceID:     ws1ID,
		IsAttached:      true,
		Status:          StatusReady,
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	retrieved, err := service.GetSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, session.ID, retrieved.ID)
	require.Equal(t, session.WorkspaceID, retrieved.WorkspaceID)
	require.Equal(t, session.IsAttached, retrieved.IsAttached)
}

func TestSessionService_GetSession_NotFound(t *testing.T) {
	service, _, _, _, _, _ := setupSessionService(t)

	ctx := context.Background()
	_, err := service.GetSession(ctx, 99999)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionService_CreateSession(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:        "session-1",
		WorkspaceID: ws1ID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, false)
	require.NoError(t, err)

	require.Equal(t, "utena-session-1", session.TmuxSessionName)
	require.Equal(t, StatusCreating, session.Status)

	waitForStatus(t, sessionStore, session.ID, StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, "session-1", retrieved.Name)
	require.False(t, retrieved.LastUsedAt.IsZero())
	require.Equal(t, ResourceReady, retrieved.Resources.Tmux.Status)
	require.True(t, tmux.HasSessionByName("utena-session-1"))
}

func TestSessionService_CreateSession_TmuxFails(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)
	tmux.SetCreateErr(fmt.Errorf("connection refused"))

	session := &Session{
		Name:        "fail-session",
		WorkspaceID: ws1ID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, false)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusBroken, 2*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusBroken, retrieved.Status)
	require.Equal(t, ResourceFailed, retrieved.Resources.Tmux.Status)
	require.Contains(t, retrieved.Resources.Tmux.Error, "connection refused")
}

func TestSessionService_CreateSession_InvalidWorkspace(t *testing.T) {
	service, _, _, _, _, _ := setupSessionService(t)

	session := &Session{
		Name:        "session-1",
		WorkspaceID: 99999,
		LastUsedAt:  time.Now(),
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionService_UpdateSession(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		TmuxSessionName: "session-1",
		Name:            "session-1",
		WorkspaceID:     ws1ID,
		IsAttached:      false,
		Status:          StatusReady,
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	session.IsAttached = true
	ctx := context.Background()
	err := service.UpdateSession(ctx, session)
	require.NoError(t, err)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.True(t, retrieved.IsAttached)
}

func TestSessionService_UpdateSession_InvalidWorkspace(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		TmuxSessionName: "session-1",
		Name:            "session-1",
		WorkspaceID:     ws1ID,
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	session.WorkspaceID = 99999
	ctx := context.Background()
	err := service.UpdateSession(ctx, session)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionService_DeleteSession(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:            "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     ws1ID,
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)
	tmux.Sessions["session-1"] = true

	ctx := context.Background()
	err := service.DeleteSession(ctx, session.ID, true)
	require.NoError(t, err)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusDeleted, retrieved.Status)
	require.Equal(t, ResourceRemoved, retrieved.Resources.Tmux.Status)
	require.False(t, tmux.HasSessionByName("session-1"))
}

func TestSessionService_DeleteSession_NotFound(t *testing.T) {
	service, _, _, _, _, _ := setupSessionService(t)

	ctx := context.Background()
	err := service.DeleteSession(ctx, 99999, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	bareDir := t.TempDir()
	dir := t.TempDir()

	bareCmds := [][]string{
		{"git", "init", "--bare"},
	}
	for _, args := range bareCmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = bareDir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v failed: %s", args, string(out))
	}

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "remote", "add", "origin", bareDir},
		{"git", "commit", "--allow-empty", "-m", "init"},
		{"git", "push", "-u", "origin", "HEAD"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v failed: %s", args, string(out))
	}
	return dir
}

func setupWorktreeSessionService(t *testing.T, repoPath string, configDir string) (*SessionService, *SessionStore, *utmux.MockRunner, uint) {
	t.Helper()

	database := setupTestDB(t)
	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
	wsGit := &workspace.Workspace{Name: "git-repo", Path: repoPath, IsGitRepo: true}
	workspaceStore.Add(wsGit)

	mock := utmux.NewMockRunner()
	tmuxService := createTmuxService(t, database, mock, bus)
	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	gitDB, err := db.OpenInMemory()
	require.NoError(t, err)
	gitDB.Migrate(&git.Repo{}, &git.Branch{}, &git.Worktree{}, &git.PullRequest{})
	t.Cleanup(func() { gitDB.Close() })
	gitService := git.NewGitService(gitDB)
	service := NewSessionService(sessionStore, workspaceService, gitService, tmuxService, bus, "eqt/", configDir)
	return service, sessionStore, mock, wsGit.ID
}

func TestSessionService_CreateSession_WithWorktree(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, mock, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	session := &Session{
		Name:        "my-feature",
		WorkspaceID: wsGitID,
		BaseBranch:  "main",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, true)
	require.NoError(t, err)

	require.Equal(t, "git-repo-my-feature", session.TmuxSessionName)

	waitForStatus(t, sessionStore, session.ID, StatusReady, 5*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)

	expectedPath := filepath.Join(repoPath, ".worktrees", "eqt-my-feature")
	require.Equal(t, expectedPath, retrieved.WorktreePath)
	require.Equal(t, "eqt/my-feature", retrieved.Branch)
	require.Equal(t, ResourceReady, retrieved.Resources.Branch.Status)
	require.Equal(t, ResourceReady, retrieved.Resources.Worktree.Status)
	require.Equal(t, ResourceReady, retrieved.Resources.WorktreeInit.Status)
	require.Equal(t, ResourceReady, retrieved.Resources.Tmux.Status)

	info, err := os.Stat(expectedPath)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	require.Equal(t, "main", retrieved.BaseBranch)
	require.True(t, mock.HasSessionByName("git-repo-my-feature"))
}

func TestSessionService_CreateSession_WithWorktree_ReusesExistingBranch(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	ctx := context.Background()
	branchName := "feature/checkout-me"
	worktreePath := filepath.Join(repoPath, ".worktrees", "feature-checkout-me")
	cmd := exec.Command("git", "-C", repoPath, "worktree", "add", "-b", branchName, worktreePath, "main")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "pre-create worktree failed: %s", string(out))

	pushCmd := exec.Command("git", "-C", worktreePath, "push", "-u", "origin", branchName)
	out, err = pushCmd.CombinedOutput()
	require.NoError(t, err, "push branch failed: %s", string(out))

	session := &Session{
		Name:        branchName,
		WorkspaceID: wsGitID,
		Branch:      branchName,
	}

	err = service.CreateSession(ctx, session, true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusReady, 5*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)

	require.Equal(t, worktreePath, retrieved.WorktreePath)
	require.Equal(t, ResourceReady, retrieved.Resources.Worktree.Status)
}

func TestSessionService_CreateSession_WithWorktree_ReusesLocalOnlyBranch(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	ctx := context.Background()
	branchName := "feature/local-only"
	worktreePath := filepath.Join(repoPath, ".worktrees", "feature-local-only")
	cmd := exec.Command("git", "-C", repoPath, "worktree", "add", "-b", branchName, worktreePath, "main")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "pre-create worktree failed: %s", string(out))

	session := &Session{
		Name:        branchName,
		WorkspaceID: wsGitID,
		Branch:      branchName,
	}

	err = service.CreateSession(ctx, session, true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusReady, 5*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)

	require.Equal(t, worktreePath, retrieved.WorktreePath)
	require.Equal(t, ResourceReady, retrieved.Resources.Branch.Status)
	require.Equal(t, ResourceReady, retrieved.Resources.Worktree.Status)
}

func TestSessionService_CreateSession_WithWorktree_InvalidBranch(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, mock, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	session := &Session{
		Name:        "my-feature",
		WorkspaceID: wsGitID,
		BaseBranch:  "nonexistent",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusBroken, 5*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusBroken, retrieved.Status)
	require.Equal(t, ResourceFailed, retrieved.Resources.Branch.Status)
	require.NotEmpty(t, retrieved.Resources.Branch.Error)
	require.False(t, mock.HasSessionByName("git-repo-my-feature"))
}

func TestSessionService_CreateSession_WithName_ComputesID(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:        "main",
		WorkspaceID: ws1ID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, false)
	require.NoError(t, err)

	require.Equal(t, "utena-main", session.TmuxSessionName)
	require.Equal(t, "main", session.Name)

	waitForStatus(t, sessionStore, session.ID, StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, "main", retrieved.Name)
	require.Equal(t, "utena-main", retrieved.TmuxSessionName)
}

func TestSessionService_CreateSession_WithName_NoWorkspace(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:        "standalone",
		WorkspaceID: ws1ID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, false)
	require.NoError(t, err)

	require.Equal(t, "utena-standalone", session.TmuxSessionName)

	waitForStatus(t, sessionStore, session.ID, StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, "standalone", retrieved.Name)
}

func TestSessionService_CreateSession_NoBranch_SkipsWorktree(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:        "my-session",
		WorkspaceID: ws1ID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, false)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Empty(t, retrieved.WorktreePath)
}

func TestSessionService_CreateSession_NonGitWorkspace_SkipsWorktree(t *testing.T) {
	database := setupTestDB(t)
	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
	wsNoGit := &workspace.Workspace{Name: "plain", Path: "/tmp/plain", IsGitRepo: false}
	workspaceStore.Add(wsNoGit)

	mock := utmux.NewMockRunner()
	tmuxService := createTmuxService(t, database, mock, bus)
	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	gitDB, err := db.OpenInMemory()
	require.NoError(t, err)
	gitDB.Migrate(&git.Repo{}, &git.Branch{}, &git.Worktree{}, &git.PullRequest{})
	t.Cleanup(func() { gitDB.Close() })
	gitService := git.NewGitService(gitDB)
	service := NewSessionService(sessionStore, workspaceService, gitService, tmuxService, bus, "eqt/", t.TempDir())

	session := &Session{
		Name:        "my-session",
		WorkspaceID: wsNoGit.ID,
		BaseBranch:  "main",
	}

	ctx := context.Background()
	err = service.CreateSession(ctx, session, false)
	require.NoError(t, err)

	require.Equal(t, "plain-my-session", session.TmuxSessionName)

	waitForStatus(t, sessionStore, session.ID, StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Empty(t, retrieved.WorktreePath)
}

func TestSessionService_CreateSession_TouchesWorkspace(t *testing.T) {
	service, _, workspaceStore, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:        "session-1",
		WorkspaceID: ws1ID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, false)
	require.NoError(t, err)

	ws, err := workspaceStore.GetByID(ws1ID)
	require.NoError(t, err)
	require.False(t, ws.LastUsedAt.IsZero())
}

func TestSessionService_ActivateSession_TouchesWorkspace(t *testing.T) {
	service, sessionStore, workspaceStore, tmux, ws1ID, _ := setupSessionService(t)

	tmux.Sessions["session-1"] = true
	session := &Session{
		Name:            "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     ws1ID,
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now().Add(-1 * time.Hour),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	_, err := service.ActivateSession(ctx, session.ID)
	require.NoError(t, err)

	ws, err := workspaceStore.GetByID(ws1ID)
	require.NoError(t, err)
	require.False(t, ws.LastUsedAt.IsZero())
}

func TestSessionService_ActivateSession_RejectsBrokenSession(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:            "broken-session",
		TmuxSessionName: "broken-session",
		WorkspaceID:     ws1ID,
		Status:          StatusBroken,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceRemoved}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	_, err := service.ActivateSession(ctx, session.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrCannotActivate)
}

func TestSessionService_ActivateSession_RecreatesMissingTmux(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:            "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     ws1ID,
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	result, err := service.ActivateSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusReady, result.Status)
	require.True(t, result.IsAttached)
	require.True(t, tmux.HasSessionByName("session-1"))
}

func TestSessionService_RefreshSession_DetectsMissingTmux(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	tmux.Sessions["session-1"] = true
	session := &Session{
		Name:            "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     ws1ID,
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	tmux.RemoveSession("session-1")

	ctx := context.Background()
	refreshed, err := service.RefreshSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusBroken, refreshed.Status)
	require.Equal(t, ResourceRemoved, refreshed.Resources.Tmux.Status)
}

func TestSessionService_RefreshSession_AllHealthy(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	tmux.Sessions["session-1"] = true
	session := &Session{
		Name:            "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     ws1ID,
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	refreshed, err := service.RefreshSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusReady, refreshed.Status)
	require.Equal(t, ResourceReady, refreshed.Resources.Tmux.Status)
}

func TestSessionService_RepairSession_RecoversBroken(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:            "broken-session",
		TmuxSessionName: "broken-session",
		WorkspaceID:     ws1ID,
		Status:          StatusBroken,
		Resources: &Resources{
			Tmux: &ResourceState{Status: ResourceRemoved},
		},
		LastUsedAt: time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	result, err := service.RepairSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusCreating, result.Status)

	waitForStatus(t, sessionStore, session.ID, StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusReady, retrieved.Status)
	require.Equal(t, ResourceReady, retrieved.Resources.Tmux.Status)
	require.True(t, tmux.HasSessionByName("broken-session"))
}

func TestSessionService_RepairSession_StillFailing(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)
	tmux.SetCreateErr(fmt.Errorf("still broken"))

	session := &Session{
		Name:            "broken-session",
		TmuxSessionName: "broken-session",
		WorkspaceID:     ws1ID,
		Status:          StatusBroken,
		Resources: &Resources{
			Tmux: &ResourceState{Status: ResourceRemoved},
		},
		LastUsedAt: time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	result, err := service.RepairSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusCreating, result.Status)

	waitForStatus(t, sessionStore, session.ID, StatusBroken, 2*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusBroken, retrieved.Status)
	require.Equal(t, ResourceFailed, retrieved.Resources.Tmux.Status)
	require.Contains(t, retrieved.Resources.Tmux.Error, "still broken")
}

func TestSessionService_RepairSession_AlreadyReady(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	tmux.Sessions["ok-session"] = true
	session := &Session{
		Name:            "ok-session",
		TmuxSessionName: "ok-session",
		WorkspaceID:     ws1ID,
		Status:          StatusBroken,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	result, err := service.RepairSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusReady, result.Status)
}

func TestSessionService_RepairSession_NotBroken(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	tmux.Sessions["session-1"] = true
	session := &Session{
		Name:            "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     ws1ID,
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	result, err := service.RepairSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusReady, result.Status)
}

func TestSessionService_Reconcile_MarksMissingTmuxBroken(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:            "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     ws1ID,
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	service.reconcileTmuxState(ctx)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusBroken, retrieved.Status)
	require.Equal(t, ResourceRemoved, retrieved.Resources.Tmux.Status)
}

func TestSessionService_Reconcile_KeepsHealthyReady(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	tmux.Sessions["session-1"] = true
	session := &Session{
		Name:            "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     ws1ID,
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	service.reconcileTmuxState(ctx)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusReady, retrieved.Status)
}

func TestSessionService_Reconcile_SkipsDeleted(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:            "deleted-1",
		TmuxSessionName: "deleted-1",
		WorkspaceID:     ws1ID,
		Status:          StatusDeleted,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceRemoved}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	service.reconcileTmuxState(ctx)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusDeleted, retrieved.Status)
}

func writeScript(t *testing.T, path string, script string) {
	t.Helper()
	dir := filepath.Dir(path)
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(path, []byte(script), 0755))
}

func TestSessionService_WorktreeInit_RunsUserLevelScript(t *testing.T) {
	repoPath := initTestRepo(t)
	configDir := t.TempDir()
	markerFile := filepath.Join(t.TempDir(), "user-marker")

	writeScript(t, filepath.Join(configDir, "worktree-setup"), fmt.Sprintf("#!/bin/sh\necho \"$UTENA_BRANCH\" > %s\n", markerFile))

	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, configDir)

	session := &Session{
		Name:        "init-test",
		WorkspaceID: wsGitID,
		BaseBranch:  "main",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusReady, 5*time.Second)

	data, err := os.ReadFile(markerFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "eqt/init-test")
}

func TestSessionService_WorktreeInit_RunsRepoLevelScript(t *testing.T) {
	repoPath := initTestRepo(t)
	markerFile := filepath.Join(t.TempDir(), "repo-marker")

	writeScript(t, filepath.Join(repoPath, ".utena", "worktree-setup"), fmt.Sprintf("#!/bin/sh\necho \"$UTENA_WORKSPACE_NAME\" > %s\n", markerFile))

	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	session := &Session{
		Name:        "repo-init",
		WorkspaceID: wsGitID,
		BaseBranch:  "main",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusReady, 5*time.Second)

	data, err := os.ReadFile(markerFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "git-repo")
}

func TestSessionService_WorktreeInit_SkipsWhenReusingWorktree(t *testing.T) {
	repoPath := initTestRepo(t)
	configDir := t.TempDir()
	markerFile := filepath.Join(t.TempDir(), "should-not-exist")

	writeScript(t, filepath.Join(configDir, "worktree-setup"), fmt.Sprintf("#!/bin/sh\ntouch %s\n", markerFile))

	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, configDir)

	branchName := "feature/reuse-me"
	worktreePath := filepath.Join(repoPath, ".worktrees", "feature-reuse-me")
	cmd := exec.Command("git", "-C", repoPath, "worktree", "add", "-b", branchName, worktreePath, "main")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "pre-create worktree failed: %s", string(out))

	pushCmd := exec.Command("git", "-C", worktreePath, "push", "-u", "origin", branchName)
	out, err = pushCmd.CombinedOutput()
	require.NoError(t, err, "push branch failed: %s", string(out))

	session := &Session{
		Name:        branchName,
		WorkspaceID: wsGitID,
		Branch:      branchName,
	}

	ctx := context.Background()
	err = service.CreateSession(ctx, session, true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusReady, 5*time.Second)

	_, err = os.Stat(markerFile)
	require.True(t, os.IsNotExist(err), "script should not have run for reused worktree")
}

func TestSessionService_WorktreeInit_FailingScriptContinues(t *testing.T) {
	repoPath := initTestRepo(t)
	configDir := t.TempDir()

	writeScript(t, filepath.Join(configDir, "worktree-setup"), "#!/bin/sh\nexit 1\n")

	service, sessionStore, mock, wsGitID := setupWorktreeSessionService(t, repoPath, configDir)

	session := &Session{
		Name:        "fail-init",
		WorkspaceID: wsGitID,
		BaseBranch:  "main",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusReady, 5*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusReady, retrieved.Status)
	require.Equal(t, ResourceReady, retrieved.Resources.WorktreeInit.Status)
	require.NotEmpty(t, retrieved.Resources.WorktreeInit.Error)
	require.True(t, mock.HasSessionByName("git-repo-fail-init"))
}

func TestSessionService_WorktreeInit_MissingScriptsSkipped(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	session := &Session{
		Name:        "no-scripts",
		WorkspaceID: wsGitID,
		BaseBranch:  "main",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusReady, 5*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusReady, retrieved.Status)
	require.Equal(t, ResourceReady, retrieved.Resources.WorktreeInit.Status)
	require.Empty(t, retrieved.Resources.WorktreeInit.Error)
}

func TestSessionService_WorktreeInit_WorkingDirIsWorktree(t *testing.T) {
	repoPath := initTestRepo(t)
	configDir := t.TempDir()
	markerFile := filepath.Join(t.TempDir(), "pwd-marker")

	writeScript(t, filepath.Join(configDir, "worktree-setup"), fmt.Sprintf("#!/bin/sh\npwd > %s\n", markerFile))

	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, configDir)

	session := &Session{
		Name:        "wd-test",
		WorkspaceID: wsGitID,
		BaseBranch:  "main",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusReady, 5*time.Second)

	data, err := os.ReadFile(markerFile)
	require.NoError(t, err)

	retrieved, _ := sessionStore.GetByID(session.ID)
	require.Contains(t, string(data), retrieved.WorktreePath)
}
