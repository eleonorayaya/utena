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
	dismissedPRStore := NewDismissedPRStore(database)
	sessionActionStore := NewSessionActionStore(database)
	service := NewSessionService(sessionStore, dismissedPRStore, sessionActionStore, workspaceService, gitService, tmuxService, bus, "eqt/", t.TempDir())
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

func TestSessionService_OnAppStart_RecoversStuckCreatingSessions(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)
	ctx := context.Background()

	stuck := &Session{
		Name:        "stuck-session",
		WorkspaceID: ws1ID,
		Status:      StatusCreating,
		LastUsedAt:  time.Now().Add(-1 * time.Hour),
	}
	require.NoError(t, sessionStore.Add(stuck))

	require.NoError(t, service.OnAppStart(ctx))

	recovered, err := sessionStore.GetByID(stuck.ID)
	require.NoError(t, err)
	require.Equal(t, StatusBroken, recovered.Status)
	require.NotEmpty(t, recovered.StatusError)
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
	session1 := &Session{Name: "session-1", WorkspaceID: ws1ID, Status: StatusActive, LastUsedAt: now.Add(-1 * time.Hour)}
	session2 := &Session{Name: "session-2", WorkspaceID: ws2ID, Status: StatusActive, LastUsedAt: now}
	sessionStore.Add(session1)
	sessionStore.Add(session2)

	ctx := context.Background()
	sessions, err := service.ListSessions(ctx)
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	require.Equal(t, "session-2", sessions[0].Name, "Most recent session should be first")
}

func TestSessionService_ListSessionsByWorkspace(t *testing.T) {
	service, sessionStore, _, _, ws1ID, ws2ID := setupSessionService(t)

	now := time.Now()
	session1 := &Session{Name: "session-1", WorkspaceID: ws1ID, Status: StatusActive, LastUsedAt: now.Add(-1 * time.Hour)}
	session2 := &Session{Name: "session-2", WorkspaceID: ws2ID, Status: StatusActive, LastUsedAt: now}
	session3 := &Session{Name: "session-3", WorkspaceID: ws1ID, Status: StatusActive, LastUsedAt: now}
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

	require.Equal(t, "session-3", sessions[0].Name, "Most recent ws-1 session should be first")
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
		Name:        "session-1",
		WorkspaceID: ws1ID,
		IsAttached:  true,
		Status:      StatusActive,
		LastUsedAt:  time.Now(),
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
	err := service.CreateSession(ctx, session, "", "", false)
	require.NoError(t, err)

	require.Equal(t, StatusCreating, session.Status)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 2*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, "session-1", retrieved.Name)
	require.False(t, retrieved.LastUsedAt.IsZero())
	require.NotNil(t, retrieved.TmuxSessionID)
	require.True(t, tmux.HasSessionByName("utena-session-1"))
}

func TestSessionService_CreateSession_DuplicateNameInWorkspace_Errors(t *testing.T) {
	service, sessionStore, _, _, ws1ID, ws2ID := setupSessionService(t)

	ctx := context.Background()
	first := &Session{Name: "dup", WorkspaceID: ws1ID}
	require.NoError(t, service.CreateSession(ctx, first, "", "", false))
	waitForStatus(t, sessionStore, first.ID, StatusActive, 2*time.Second)

	second := &Session{Name: "dup", WorkspaceID: ws1ID}
	err := service.CreateSession(ctx, second, "", "", false)
	require.ErrorIs(t, err, ErrSessionAlreadyExists)
	require.Zero(t, second.ID, "duplicate session should not be persisted")

	otherWorkspace := &Session{Name: "dup", WorkspaceID: ws2ID}
	require.NoError(t, service.CreateSession(ctx, otherWorkspace, "", "", false), "same name in a different workspace must be allowed")
}

func TestSessionService_CreateSession_DuplicateNameAfterDelete_Allowed(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	ctx := context.Background()
	first := &Session{Name: "reusable", WorkspaceID: ws1ID}
	require.NoError(t, service.CreateSession(ctx, first, "", "", false))
	waitForStatus(t, sessionStore, first.ID, StatusActive, 2*time.Second)
	require.NoError(t, service.DeleteSession(ctx, first.ID, false, false))

	deleted, err := sessionStore.GetByID(first.ID)
	require.NoError(t, err)
	require.Nil(t, deleted.TmuxSessionID, "deleted session should not retain tmux_session_id")

	second := &Session{Name: "reusable", WorkspaceID: ws1ID}
	require.NoError(t, service.CreateSession(ctx, second, "", "", false))
	waitForStatus(t, sessionStore, second.ID, StatusActive, 2*time.Second)
}

func TestSessionService_CreateSession_TmuxFails(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)
	tmux.SetCreateErr(fmt.Errorf("connection refused"))

	session := &Session{
		Name:        "fail-session",
		WorkspaceID: ws1ID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "", false)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusBroken, 2*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusBroken, retrieved.Status)
	require.Contains(t, retrieved.StatusError, "tmux")
}

func TestSessionService_CreateSession_InvalidWorkspace(t *testing.T) {
	service, _, _, _, _, _ := setupSessionService(t)

	session := &Session{
		Name:        "session-1",
		WorkspaceID: 99999,
		LastUsedAt:  time.Now(),
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "", false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionService_UpdateSession(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:        "session-1",
		WorkspaceID: ws1ID,
		IsAttached:  false,
		Status:      StatusActive,
		LastUsedAt:  time.Now(),
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
		Name:        "session-1",
		WorkspaceID: ws1ID,
		LastUsedAt:  time.Now(),
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

	tmux.Sessions["session-1"] = true
	session := &Session{
		Name:        "session-1",
		WorkspaceID: ws1ID,
		Status:      StatusActive,
		LastUsedAt:  time.Now(),
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "", false)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 2*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.NotNil(t, retrieved.TmuxSessionID)

	err = service.DeleteSession(ctx, retrieved.ID, true, false)
	require.NoError(t, err)

	retrieved, err = sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusDeleted, retrieved.Status)
}

func TestSessionService_DeleteSession_NotFound(t *testing.T) {
	service, _, _, _, _, _ := setupSessionService(t)

	ctx := context.Background()
	err := service.DeleteSession(ctx, 99999, true, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionService_DeleteSession_Creating_Blocked(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	sess := &Session{
		Name:        "stuck-session",
		WorkspaceID: ws1ID,
		Status:      StatusCreating,
		LastUsedAt:  time.Now(),
	}

	ctx := context.Background()
	err := sessionStore.Add(sess)
	require.NoError(t, err)

	err = service.DeleteSession(ctx, sess.ID, true, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot delete session while it is being created")
}

func TestSessionService_DeleteSession_Creating_Force(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	sess := &Session{
		Name:        "stuck-session",
		WorkspaceID: ws1ID,
		Status:      StatusCreating,
		LastUsedAt:  time.Now(),
	}

	ctx := context.Background()
	err := sessionStore.Add(sess)
	require.NoError(t, err)

	err = service.DeleteSession(ctx, sess.ID, true, true)
	require.NoError(t, err)

	retrieved, err := sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.Equal(t, StatusDeleted, retrieved.Status)
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	bareDir := t.TempDir()
	dir := t.TempDir()

	bareCmds := [][]string{
		{"git", "init", "--bare", "-b", "main"},
	}
	for _, args := range bareCmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = bareDir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v failed: %s", args, string(out))
	}

	cmds := [][]string{
		{"git", "init", "-b", "main"},
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
	service, sessionStore, mock, wsID, _, _ := setupWorktreeSessionServiceFull(t, repoPath, configDir)
	return service, sessionStore, mock, wsID
}

func setupWorktreeSessionServiceFull(t *testing.T, repoPath string, configDir string) (*SessionService, *SessionStore, *utmux.MockRunner, uint, db.Database, *git.GitService) {
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
	gitService := git.NewGitService(database)
	dismissedPRStore := NewDismissedPRStore(database)
	sessionActionStore := NewSessionActionStore(database)
	service := NewSessionService(sessionStore, dismissedPRStore, sessionActionStore, workspaceService, gitService, tmuxService, bus, "eqt/", configDir)
	return service, sessionStore, mock, wsGit.ID, database, gitService
}

func TestSessionService_CreateSession_WithWorktree(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, mock, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	session := &Session{
		Name:        "my-feature",
		WorkspaceID: wsGitID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "main", true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)

	expectedPath := filepath.Join(repoPath, ".worktrees", "eqt-my-feature")

	info, err := os.Stat(expectedPath)
	require.NoError(t, err)
	require.True(t, info.IsDir())

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
	}

	err = service.CreateSession(ctx, session, branchName, "", true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)
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
	}

	err = service.CreateSession(ctx, session, branchName, "", true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)
}

func TestSessionService_CreateSession_TimesOut(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())
	service.setupTimeout = 1 * time.Nanosecond

	session := &Session{
		Name:        "slow-feature",
		WorkspaceID: wsGitID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "main", true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusBroken, 5*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusBroken, retrieved.Status)
	require.Contains(t, retrieved.StatusError, "timed out")
}

func TestSessionService_CreateSession_WithWorktree_InvalidBranch(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, mock, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	session := &Session{
		Name:        "my-feature",
		WorkspaceID: wsGitID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "nonexistent", true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusBroken, 5*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusBroken, retrieved.Status)
	require.Contains(t, retrieved.StatusError, "branch")
	require.False(t, mock.HasSessionByName("git-repo-my-feature"))
}

func TestSessionService_CreateSession_WithName_ComputesID(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:        "main",
		WorkspaceID: ws1ID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "", false)
	require.NoError(t, err)

	require.Equal(t, "main", session.Name)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 2*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, "main", retrieved.Name)
	require.NotNil(t, retrieved.TmuxSessionID)
	require.True(t, tmux.HasSessionByName("utena-main"))
}

func TestSessionService_CreateSession_WithName_NoWorkspace(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:        "standalone",
		WorkspaceID: ws1ID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "", false)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 2*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, "standalone", retrieved.Name)
	require.NotNil(t, retrieved.TmuxSessionID)
}

func TestSessionService_CreateSession_NoBranch_SkipsWorktree(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:        "my-session",
		WorkspaceID: ws1ID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "", false)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 2*time.Second)
}

func TestSessionService_CreateSession_ExistingBranch_AutoCreatesWorktree(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	ctx := context.Background()
	branchName := "eqt/auto-worktree"
	worktreePath := filepath.Join(repoPath, ".worktrees", "eqt-auto-worktree")
	cmd := exec.Command("git", "-C", repoPath, "worktree", "add", "-b", branchName, worktreePath, "main")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "pre-create worktree failed: %s", string(out))

	pushCmd := exec.Command("git", "-C", worktreePath, "push", "-u", "origin", branchName)
	out, err = pushCmd.CombinedOutput()
	require.NoError(t, err, "push branch failed: %s", string(out))

	session := &Session{WorkspaceID: wsGitID}

	err = service.CreateSession(ctx, session, branchName, "", false)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)

	_, err = os.Stat(worktreePath)
	require.NoError(t, err, "worktree should exist")

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, "auto-worktree", retrieved.Name, "branch prefix should be stripped from session name")
}

func TestSessionService_CreateSession_ExistingMainBranch_SkipsWorktree(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	session := &Session{WorkspaceID: wsGitID}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "main", "", false)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)

	_, err = os.Stat(filepath.Join(repoPath, ".worktrees", "main"))
	require.True(t, os.IsNotExist(err), "no worktree should be created for main")
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
	dismissedPRStore := NewDismissedPRStore(database)
	sessionActionStore := NewSessionActionStore(database)
	service := NewSessionService(sessionStore, dismissedPRStore, sessionActionStore, workspaceService, gitService, tmuxService, bus, "eqt/", t.TempDir())

	session := &Session{
		Name:        "my-session",
		WorkspaceID: wsNoGit.ID,
	}

	ctx := context.Background()
	err = service.CreateSession(ctx, session, "", "main", false)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 2*time.Second)
}

func TestSessionService_CreateSession_TouchesWorkspace(t *testing.T) {
	service, _, workspaceStore, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:        "session-1",
		WorkspaceID: ws1ID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "", false)
	require.NoError(t, err)

	ws, err := workspaceStore.GetByID(ws1ID)
	require.NoError(t, err)
	require.False(t, ws.LastUsedAt.IsZero())
}

func TestSessionService_ActivateSession_TouchesWorkspace(t *testing.T) {
	service, sessionStore, workspaceStore, tmux, ws1ID, _ := setupSessionService(t)

	tmux.Sessions["utena-session-1"] = true
	session := &Session{
		Name:        "session-1",
		WorkspaceID: ws1ID,
		Status:      StatusActive,
		LastUsedAt:  time.Now().Add(-1 * time.Hour),
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
		Name:        "broken-session",
		WorkspaceID: ws1ID,
		Status:      StatusBroken,
		LastUsedAt:  time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	_, err := service.ActivateSession(ctx, session.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrCannotActivate)
}

func TestSessionService_ActivateSession_PendingPR_CreatesWorktree(t *testing.T) {
	repoPath := initTestRepo(t)
	branchName := "harleyk--catalog-docs"

	pushCmd := exec.Command("git", "-C", repoPath, "push", "origin", "main:"+branchName)
	out, err := pushCmd.CombinedOutput()
	require.NoError(t, err, "push remote branch failed: %s", string(out))

	service, sessionStore, mock, wsGitID, gitDB, _ := setupWorktreeSessionServiceFull(t, repoPath, t.TempDir())

	repo := &git.Repo{Path: repoPath, FullName: "owner/repo"}
	require.NoError(t, gitDB.Create(repo).Error)
	branch := &git.Branch{Name: branchName, RepoID: repo.ID, ExistsLocal: false, ExistsRemote: true}
	require.NoError(t, gitDB.Create(branch).Error)

	branchID := branch.ID
	pending := &Session{
		Name:        branchName,
		WorkspaceID: wsGitID,
		BranchID:    &branchID,
		Status:      StatusPending,
		LastUsedAt:  time.Now(),
	}
	require.NoError(t, sessionStore.Add(pending))

	ctx := context.Background()
	result, err := service.ActivateSession(ctx, pending.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, result.Status)

	worktreePath := filepath.Join(repoPath, ".worktrees", "harleyk--catalog-docs")
	info, err := os.Stat(worktreePath)
	require.NoError(t, err, "expected worktree at %s", worktreePath)
	require.True(t, info.IsDir())

	tmuxName := fmt.Sprintf("git-repo-%s", branchName)
	require.True(t, mock.HasSessionByName(tmuxName), "tmux session %q not created", tmuxName)
	ts, err := service.tmuxService.GetSessionByName(tmuxName)
	require.NoError(t, err)
	require.Equal(t, worktreePath, ts.StartDir, "tmux session start dir should be the worktree, not the repo root")
}

func TestSessionService_ActivateSession_RecreatesMissingTmux(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:        "session-1",
		WorkspaceID: ws1ID,
		Status:      StatusActive,
		LastUsedAt:  time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	result, err := service.ActivateSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, result.Status)
	require.True(t, result.IsAttached)
	require.True(t, tmux.HasSessionByName("utena-session-1"))
}

func TestSessionService_RefreshSession_DetectsMissingTmux(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:        "session-1",
		WorkspaceID: ws1ID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "", false)
	require.NoError(t, err)
	waitForStatus(t, sessionStore, session.ID, StatusActive, 2*time.Second)

	tmux.RemoveSession("utena-session-1")
	service.tmuxService.HandleSessionClosed(ctx, "utena-session-1")

	refreshed, err := service.RefreshSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusInactive, refreshed.Status)
}

func TestSessionService_RefreshSession_AllHealthy(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:        "session-1",
		WorkspaceID: ws1ID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "", false)
	require.NoError(t, err)
	waitForStatus(t, sessionStore, session.ID, StatusActive, 2*time.Second)

	refreshed, err := service.RefreshSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, refreshed.Status)
}

func TestSessionService_RepairSession_RecoversBroken(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:        "broken-session",
		WorkspaceID: ws1ID,
		Status:      StatusBroken,
		LastUsedAt:  time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	result, err := service.RepairSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusCreating, result.Status)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 2*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, retrieved.Status)
	require.True(t, tmux.HasSessionByName("utena-broken-session"))
}

func TestSessionService_RepairSession_StillFailing(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)
	tmux.SetCreateErr(fmt.Errorf("still broken"))

	session := &Session{
		Name:        "broken-session",
		WorkspaceID: ws1ID,
		Status:      StatusBroken,
		LastUsedAt:  time.Now(),
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
	require.Contains(t, retrieved.StatusError, "still broken")
}

func TestSessionService_RepairSession_AlreadyReady(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	tmux.Sessions["utena-ok-session"] = true
	session := &Session{
		Name:        "ok-session",
		WorkspaceID: ws1ID,
		Status:      StatusBroken,
		LastUsedAt:  time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	result, err := service.RepairSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusCreating, result.Status)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 2*time.Second)
}

func TestSessionService_RepairSession_NotBroken(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	tmux.Sessions["utena-session-1"] = true
	session := &Session{
		Name:        "session-1",
		WorkspaceID: ws1ID,
		Status:      StatusActive,
		LastUsedAt:  time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	_, err := service.RepairSession(ctx, session.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrSessionNotBroken)
}

func TestSessionService_Reconcile_MarksMissingTmuxBroken(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:        "session-1",
		WorkspaceID: ws1ID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "", false)
	require.NoError(t, err)
	waitForStatus(t, sessionStore, session.ID, StatusActive, 2*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.NotNil(t, retrieved.TmuxSessionID)

	tmux.RemoveSession("utena-session-1")
	service.tmuxService.HandleSessionClosed(ctx, "utena-session-1")

	service.reconcileTmuxState(ctx)

	retrieved, err = sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusInactive, retrieved.Status)
}

func TestSessionService_Reconcile_KeepsHealthyReady(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:        "session-1",
		WorkspaceID: ws1ID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "", false)
	require.NoError(t, err)
	waitForStatus(t, sessionStore, session.ID, StatusActive, 2*time.Second)

	service.reconcileTmuxState(ctx)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, retrieved.Status)
}

func TestSessionService_Reconcile_SkipsDeleted(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:        "deleted-1",
		WorkspaceID: ws1ID,
		Status:      StatusDeleted,
		LastUsedAt:  time.Now(),
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
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "main", true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)

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
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "main", true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)

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
	}

	ctx := context.Background()
	err = service.CreateSession(ctx, session, branchName, "", true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)

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
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "main", true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, retrieved.Status)
	require.True(t, mock.HasSessionByName("git-repo-fail-init"))
}

func TestSessionService_WorktreeInit_MissingScriptsSkipped(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	session := &Session{
		Name:        "no-scripts",
		WorkspaceID: wsGitID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "main", true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, retrieved.Status)
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
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "main", true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)

	data, err := os.ReadFile(markerFile)
	require.NoError(t, err)

	expectedPath := filepath.Join(repoPath, ".worktrees", "eqt-wd-test")
	require.Contains(t, string(data), expectedPath)
}

func TestSessionService_CreateSession_DirtyMainRepo_SetsWarning(t *testing.T) {
	repoPath := initTestRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "dirty.txt"), []byte("uncommitted"), 0644))

	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	sess := &Session{Name: "warn-feature", WorkspaceID: wsGitID}
	ctx := context.Background()
	require.NoError(t, service.CreateSession(ctx, sess, "", "main", true))

	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	retrieved, err := sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, retrieved.Status)
	require.Contains(t, retrieved.StatusError, "branch not pulled")
}

func TestSessionService_CreateSession_CleanRepo_NoWarning(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	sess := &Session{Name: "clean-feature", WorkspaceID: wsGitID}
	ctx := context.Background()
	require.NoError(t, service.CreateSession(ctx, sess, "", "main", true))

	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	retrieved, err := sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, retrieved.Status)
	require.Empty(t, retrieved.StatusError)
}

func TestSessionService_RepairSession_ClearsWarning(t *testing.T) {
	repoPath := initTestRepo(t)
	dirtyFile := filepath.Join(repoPath, "dirty.txt")
	require.NoError(t, os.WriteFile(dirtyFile, []byte("uncommitted"), 0644))

	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	sess := &Session{Name: "warn-repair", WorkspaceID: wsGitID}
	ctx := context.Background()
	require.NoError(t, service.CreateSession(ctx, sess, "", "main", true))
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	warned, err := sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.NotEmpty(t, warned.StatusError)

	require.NoError(t, os.Remove(dirtyFile))

	_, err = service.RepairSession(ctx, sess.ID)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	retrieved, err := sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.Empty(t, retrieved.StatusError)
}

func setupBareWorktreeSessionService(t *testing.T, configDir string) (*SessionService, *SessionStore, *utmux.MockRunner, uint, string) {
	t.Helper()

	repoPath := initTestRepo(t)

	database := setupTestDB(t)
	gitService := git.NewGitService(database)
	ctx := context.Background()
	require.NoError(t, gitService.MigrateToBare(ctx, repoPath))

	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
	wsGit := &workspace.Workspace{Name: "git-repo", Path: repoPath, IsGitRepo: true, IsBare: true}
	workspaceStore.Add(wsGit)

	mock := utmux.NewMockRunner()
	tmuxService := createTmuxService(t, database, mock, bus)
	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	dismissedPRStore := NewDismissedPRStore(database)
	sessionActionStore := NewSessionActionStore(database)
	service := NewSessionService(sessionStore, dismissedPRStore, sessionActionStore, workspaceService, gitService, tmuxService, bus, "eqt/", configDir)
	return service, sessionStore, mock, wsGit.ID, repoPath
}

func TestRunSetup_BareWorkspace_LinksClaudeSettings(t *testing.T) {
	service, sessionStore, _, wsID, repoPath := setupBareWorktreeSessionService(t, t.TempDir())

	session := &Session{
		Name:        "my-feature",
		WorkspaceID: wsID,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, "", "main", true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)

	worktreePath := filepath.Join(repoPath, "eqt-my-feature")

	rootSettings := filepath.Join(repoPath, ".claude", "settings.local.json")
	data, err := os.ReadFile(rootSettings)
	require.NoError(t, err, "workspace root settings.local.json should exist")
	require.NotEmpty(t, data)

	linkPath := filepath.Join(worktreePath, ".claude", "settings.local.json")
	target, err := os.Readlink(linkPath)
	require.NoError(t, err, "worktree settings.local.json should be a symlink")
	require.False(t, filepath.IsAbs(target), "symlink target should be relative, got %q", target)

	resolved := filepath.Join(filepath.Dir(linkPath), target)
	gotAbs, err := filepath.EvalSymlinks(resolved)
	require.NoError(t, err)
	wantAbs, err := filepath.EvalSymlinks(rootSettings)
	require.NoError(t, err)
	require.Equal(t, wantAbs, gotAbs, "symlink should resolve to workspace root settings")
}
