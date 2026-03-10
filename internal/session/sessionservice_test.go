package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type mockTmuxManager struct {
	mu        sync.Mutex
	sessions  map[string]bool
	createErr error
	killErr   error
}

func newMockTmuxManager() *mockTmuxManager {
	return &mockTmuxManager{sessions: make(map[string]bool)}
}

func (m *mockTmuxManager) CreateSession(name, startDir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return m.createErr
	}
	m.sessions[name] = true
	return nil
}

func (m *mockTmuxManager) KillSession(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.killErr != nil {
		return m.killErr
	}
	delete(m.sessions, name)
	return nil
}

func (m *mockTmuxManager) HasSession(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[name]
}

func (m *mockTmuxManager) ListSessionNames() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.sessions))
	for name := range m.sessions {
		names = append(names, name)
	}
	return names, nil
}

func (m *mockTmuxManager) setCreateErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createErr = err
}

func (m *mockTmuxManager) removeSession(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, name)
}

func setupSessionService(t *testing.T) (*SessionService, *SessionStore, *workspace.WorkspaceStore, *mockTmuxManager) {
	t.Helper()

	database := setupTestDB(t)
	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")

	workspaceStore.Add(&workspace.Workspace{ID: "ws-1", Name: "utena", Path: "/tmp/utena"})
	workspaceStore.Add(&workspace.Workspace{ID: "ws-2", Name: "other", Path: "/tmp/other"})

	tmux := newMockTmuxManager()
	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	gitService := git.NewGitService()
	service := NewSessionService(sessionStore, workspaceService, gitService, tmux, bus, "eqt/")
	return service, sessionStore, workspaceStore, tmux
}

func waitForStatus(t *testing.T, store *SessionStore, id string, status SessionStatus, timeout time.Duration) {
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
	t.Fatalf("session %q did not reach status %q within %v (current: %q)", id, status, timeout, sess.Status)
}

func TestNewSessionService(t *testing.T) {
	service, _, _, _ := setupSessionService(t)
	require.NotNil(t, service)
	require.NotNil(t, service.store)
	require.NotNil(t, service.workspaceService)
}

func TestSessionService_OnAppStart(t *testing.T) {
	service, _, _, _ := setupSessionService(t)
	ctx := context.Background()
	err := service.OnAppStart(ctx)
	require.NoError(t, err)
}

func TestSessionService_OnAppEnd(t *testing.T) {
	service, _, _, _ := setupSessionService(t)
	ctx := context.Background()
	err := service.OnAppEnd(ctx)
	require.NoError(t, err)
}

func TestSessionService_ListSessions(t *testing.T) {
	service, sessionStore, _, _ := setupSessionService(t)

	now := time.Now()
	session1 := &Session{ID: "session-1", TmuxSessionName: "session-1", Name: "session-1", WorkspaceID: "ws-1", Status: StatusReady, LastUsedAt: now.Add(-1 * time.Hour)}
	session2 := &Session{ID: "session-2", TmuxSessionName: "session-2", Name: "session-2", WorkspaceID: "ws-2", Status: StatusReady, LastUsedAt: now}
	sessionStore.Add(session1)
	sessionStore.Add(session2)

	ctx := context.Background()
	sessions, err := service.ListSessions(ctx)
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	require.Equal(t, "session-2", sessions[0].ID, "Most recent session should be first")
}

func TestSessionService_ListSessionsByWorkspace(t *testing.T) {
	service, sessionStore, _, _ := setupSessionService(t)

	now := time.Now()
	session1 := &Session{ID: "session-1", TmuxSessionName: "session-1", Name: "session-1", WorkspaceID: "ws-1", Status: StatusReady, LastUsedAt: now.Add(-1 * time.Hour)}
	session2 := &Session{ID: "session-2", TmuxSessionName: "session-2", Name: "session-2", WorkspaceID: "ws-2", Status: StatusReady, LastUsedAt: now}
	session3 := &Session{ID: "session-3", TmuxSessionName: "session-3", Name: "session-3", WorkspaceID: "ws-1", Status: StatusReady, LastUsedAt: now}
	sessionStore.Add(session1)
	sessionStore.Add(session2)
	sessionStore.Add(session3)

	ctx := context.Background()
	sessions, err := service.ListSessionsByWorkspace(ctx, "ws-1")
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	for _, session := range sessions {
		require.Equal(t, "ws-1", session.WorkspaceID)
	}

	require.Equal(t, "session-3", sessions[0].ID, "Most recent ws-1 session should be first")
}

func TestSessionService_ListSessionsByWorkspace_InvalidWorkspace(t *testing.T) {
	service, _, _, _ := setupSessionService(t)

	ctx := context.Background()
	_, err := service.ListSessionsByWorkspace(ctx, "nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionService_GetSession(t *testing.T) {
	service, sessionStore, _, _ := setupSessionService(t)

	session := &Session{
		ID:              "session-1",
		TmuxSessionName: "session-1",
		Name:            "session-1",
		WorkspaceID:     "ws-1",
		IsAttached:      true,
		Status:          StatusReady,
		LastUsedAt:      time.Now(),
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
	service, _, _, _ := setupSessionService(t)

	ctx := context.Background()
	_, err := service.GetSession(ctx, "nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionService_CreateSession(t *testing.T) {
	service, sessionStore, _, tmux := setupSessionService(t)

	session := &Session{
		Name:        "session-1",
		WorkspaceID: "ws-1",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, false)
	require.NoError(t, err)

	require.Equal(t, "utena-session-1", session.ID)
	require.Equal(t, StatusCreating, session.Status)

	waitForStatus(t, sessionStore, "utena-session-1", StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID("utena-session-1")
	require.NoError(t, err)
	require.Equal(t, "session-1", retrieved.Name)
	require.False(t, retrieved.LastUsedAt.IsZero())
	require.Equal(t, ResourceReady, retrieved.Resources.Tmux.Status)
	require.True(t, tmux.HasSession("utena-session-1"))
}

func TestSessionService_CreateSession_TmuxFails(t *testing.T) {
	service, sessionStore, _, tmux := setupSessionService(t)
	tmux.setCreateErr(fmt.Errorf("connection refused"))

	session := &Session{
		Name:        "fail-session",
		WorkspaceID: "ws-1",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, false)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, "utena-fail-session", StatusBroken, 2*time.Second)

	retrieved, err := sessionStore.GetByID("utena-fail-session")
	require.NoError(t, err)
	require.Equal(t, StatusBroken, retrieved.Status)
	require.Equal(t, ResourceFailed, retrieved.Resources.Tmux.Status)
	require.Contains(t, retrieved.Resources.Tmux.Error, "connection refused")
}

func TestSessionService_CreateSession_InvalidWorkspace(t *testing.T) {
	service, _, _, _ := setupSessionService(t)

	session := &Session{
		Name:        "session-1",
		WorkspaceID: "nonexistent",
		LastUsedAt:  time.Now(),
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionService_UpdateSession(t *testing.T) {
	service, sessionStore, _, _ := setupSessionService(t)

	session := &Session{
		ID:              "session-1",
		TmuxSessionName: "session-1",
		Name:            "session-1",
		WorkspaceID:     "ws-1",
		IsAttached:      false,
		Status:          StatusReady,
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	session.IsAttached = true
	ctx := context.Background()
	err := service.UpdateSession(ctx, session)
	require.NoError(t, err)

	retrieved, err := sessionStore.GetByID("session-1")
	require.NoError(t, err)
	require.True(t, retrieved.IsAttached)
}

func TestSessionService_UpdateSession_InvalidWorkspace(t *testing.T) {
	service, sessionStore, _, _ := setupSessionService(t)

	session := &Session{
		ID:              "session-1",
		TmuxSessionName: "session-1",
		Name:            "session-1",
		WorkspaceID:     "ws-1",
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	session.WorkspaceID = "nonexistent"
	ctx := context.Background()
	err := service.UpdateSession(ctx, session)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionService_DeleteSession(t *testing.T) {
	service, sessionStore, _, tmux := setupSessionService(t)

	session := &Session{
		ID:              "session-1",
		Name:            "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     "ws-1",
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)
	tmux.sessions["session-1"] = true

	ctx := context.Background()
	err := service.DeleteSession(ctx, "session-1", true)
	require.NoError(t, err)

	retrieved, err := sessionStore.GetByID("session-1")
	require.NoError(t, err)
	require.Equal(t, StatusDeleted, retrieved.Status)
	require.Equal(t, ResourceRemoved, retrieved.Resources.Tmux.Status)
	require.False(t, tmux.HasSession("session-1"))
}

func TestSessionService_DeleteSession_NotFound(t *testing.T) {
	service, _, _, _ := setupSessionService(t)

	ctx := context.Background()
	err := service.DeleteSession(ctx, "nonexistent", true)
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

func TestSessionService_CreateSession_WithWorktree(t *testing.T) {
	repoPath := initTestRepo(t)

	database := setupTestDB(t)
	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
	workspaceStore.Add(&workspace.Workspace{ID: "ws-git", Name: "git-repo", Path: repoPath, IsGitRepo: true})

	tmux := newMockTmuxManager()
	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	gitService := git.NewGitService()
	service := NewSessionService(sessionStore, workspaceService, gitService, tmux, bus, "eqt/")

	session := &Session{
		Name:          "my-feature",
		WorkspaceID:   "ws-git",
		BaseBranch:    "main",
		BranchCreated: true,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, true)
	require.NoError(t, err)

	require.Equal(t, "git-repo-my-feature", session.ID)

	waitForStatus(t, sessionStore, "git-repo-my-feature", StatusReady, 5*time.Second)

	retrieved, err := sessionStore.GetByID("git-repo-my-feature")
	require.NoError(t, err)

	expectedPath := filepath.Join(repoPath, ".worktrees", "eqt-my-feature")
	require.Equal(t, expectedPath, retrieved.WorktreePath)
	require.Equal(t, "eqt/my-feature", retrieved.Branch)
	require.Equal(t, ResourceReady, retrieved.Resources.Branch.Status)
	require.Equal(t, ResourceReady, retrieved.Resources.Worktree.Status)
	require.Equal(t, ResourceReady, retrieved.Resources.Tmux.Status)

	info, err := os.Stat(expectedPath)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	require.Equal(t, "main", retrieved.BaseBranch)
	require.True(t, tmux.HasSession("git-repo-my-feature"))
}

func TestSessionService_CreateSession_WithWorktree_InvalidBranch(t *testing.T) {
	repoPath := initTestRepo(t)

	database := setupTestDB(t)
	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
	workspaceStore.Add(&workspace.Workspace{ID: "ws-git", Name: "git-repo", Path: repoPath, IsGitRepo: true})

	tmux := newMockTmuxManager()
	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	gitService := git.NewGitService()
	service := NewSessionService(sessionStore, workspaceService, gitService, tmux, bus, "eqt/")

	session := &Session{
		Name:          "my-feature",
		WorkspaceID:   "ws-git",
		BaseBranch:    "nonexistent",
		BranchCreated: true,
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, true)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, "git-repo-my-feature", StatusBroken, 5*time.Second)

	retrieved, err := sessionStore.GetByID("git-repo-my-feature")
	require.NoError(t, err)
	require.Equal(t, StatusBroken, retrieved.Status)
	require.Equal(t, ResourceFailed, retrieved.Resources.Branch.Status)
	require.NotEmpty(t, retrieved.Resources.Branch.Error)
	require.False(t, tmux.HasSession("git-repo-my-feature"))
}

func TestSessionService_CreateSession_WithName_ComputesID(t *testing.T) {
	service, sessionStore, _, _ := setupSessionService(t)

	session := &Session{
		Name:        "main",
		WorkspaceID: "ws-1",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, false)
	require.NoError(t, err)

	require.Equal(t, "utena-main", session.ID)
	require.Equal(t, "main", session.Name)

	waitForStatus(t, sessionStore, "utena-main", StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID("utena-main")
	require.NoError(t, err)
	require.Equal(t, "main", retrieved.Name)
	require.Equal(t, "utena-main", retrieved.ID)
}

func TestSessionService_CreateSession_WithName_NoWorkspace(t *testing.T) {
	service, sessionStore, _, _ := setupSessionService(t)

	session := &Session{
		Name:        "standalone",
		WorkspaceID: "ws-1",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, false)
	require.NoError(t, err)

	require.Equal(t, "utena-standalone", session.ID)

	waitForStatus(t, sessionStore, "utena-standalone", StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID("utena-standalone")
	require.NoError(t, err)
	require.Equal(t, "standalone", retrieved.Name)
}

func TestSessionService_CreateSession_NoBranch_SkipsWorktree(t *testing.T) {
	service, sessionStore, _, _ := setupSessionService(t)

	session := &Session{
		Name:        "my-session",
		WorkspaceID: "ws-1",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, false)
	require.NoError(t, err)

	waitForStatus(t, sessionStore, "utena-my-session", StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID("utena-my-session")
	require.NoError(t, err)
	require.Empty(t, retrieved.WorktreePath)
}

func TestSessionService_CreateSession_NonGitWorkspace_SkipsWorktree(t *testing.T) {
	database := setupTestDB(t)
	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
	workspaceStore.Add(&workspace.Workspace{ID: "ws-nogit", Name: "plain", Path: "/tmp/plain", IsGitRepo: false})

	tmux := newMockTmuxManager()
	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	gitService := git.NewGitService()
	service := NewSessionService(sessionStore, workspaceService, gitService, tmux, bus, "eqt/")

	session := &Session{
		Name:        "my-session",
		WorkspaceID: "ws-nogit",
		BaseBranch:  "main",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, false)
	require.NoError(t, err)

	require.Equal(t, "plain-my-session", session.ID)

	waitForStatus(t, sessionStore, "plain-my-session", StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID("plain-my-session")
	require.NoError(t, err)
	require.Empty(t, retrieved.WorktreePath)
}

func TestSessionService_CreateSession_TouchesWorkspace(t *testing.T) {
	service, _, workspaceStore, _ := setupSessionService(t)

	session := &Session{
		Name:        "session-1",
		WorkspaceID: "ws-1",
	}

	ctx := context.Background()
	err := service.CreateSession(ctx, session, false)
	require.NoError(t, err)

	ws, err := workspaceStore.GetByID("ws-1")
	require.NoError(t, err)
	require.False(t, ws.LastUsedAt.IsZero())
}

func TestSessionService_ActivateSession_TouchesWorkspace(t *testing.T) {
	service, sessionStore, workspaceStore, tmux := setupSessionService(t)

	tmux.sessions["session-1"] = true
	session := &Session{
		ID:              "session-1",
		Name:            "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     "ws-1",
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now().Add(-1 * time.Hour),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	_, err := service.ActivateSession(ctx, "session-1")
	require.NoError(t, err)

	ws, err := workspaceStore.GetByID("ws-1")
	require.NoError(t, err)
	require.False(t, ws.LastUsedAt.IsZero())
}

func TestSessionService_ActivateSession_RejectsBrokenSession(t *testing.T) {
	service, sessionStore, _, _ := setupSessionService(t)

	session := &Session{
		ID:              "broken-session",
		Name:            "broken-session",
		TmuxSessionName: "broken-session",
		WorkspaceID:     "ws-1",
		Status:          StatusBroken,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceRemoved}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	_, err := service.ActivateSession(ctx, "broken-session")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrCannotActivate)
}

func TestSessionService_ActivateSession_RecreatesMissingTmux(t *testing.T) {
	service, sessionStore, _, tmux := setupSessionService(t)

	session := &Session{
		ID:              "session-1",
		Name:            "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     "ws-1",
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	result, err := service.ActivateSession(ctx, "session-1")
	require.NoError(t, err)
	require.Equal(t, StatusReady, result.Status)
	require.True(t, result.IsAttached)
	require.True(t, tmux.HasSession("session-1"))
}

func TestSessionService_RefreshSession_DetectsMissingTmux(t *testing.T) {
	service, sessionStore, _, tmux := setupSessionService(t)

	tmux.sessions["session-1"] = true
	session := &Session{
		ID:              "session-1",
		Name:            "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     "ws-1",
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	tmux.removeSession("session-1")

	ctx := context.Background()
	refreshed, err := service.RefreshSession(ctx, "session-1")
	require.NoError(t, err)
	require.Equal(t, StatusBroken, refreshed.Status)
	require.Equal(t, ResourceRemoved, refreshed.Resources.Tmux.Status)
}

func TestSessionService_RefreshSession_AllHealthy(t *testing.T) {
	service, sessionStore, _, tmux := setupSessionService(t)

	tmux.sessions["session-1"] = true
	session := &Session{
		ID:              "session-1",
		Name:            "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     "ws-1",
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	refreshed, err := service.RefreshSession(ctx, "session-1")
	require.NoError(t, err)
	require.Equal(t, StatusReady, refreshed.Status)
	require.Equal(t, ResourceReady, refreshed.Resources.Tmux.Status)
}

func TestSessionService_RepairSession_RecoversBroken(t *testing.T) {
	service, sessionStore, _, tmux := setupSessionService(t)

	session := &Session{
		ID:              "broken-session",
		Name:            "broken-session",
		TmuxSessionName: "broken-session",
		WorkspaceID:     "ws-1",
		Status:          StatusBroken,
		Resources: &Resources{
			Tmux: &ResourceState{Status: ResourceRemoved},
		},
		LastUsedAt: time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	result, err := service.RepairSession(ctx, "broken-session")
	require.NoError(t, err)
	require.Equal(t, StatusCreating, result.Status)

	waitForStatus(t, sessionStore, "broken-session", StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID("broken-session")
	require.NoError(t, err)
	require.Equal(t, StatusReady, retrieved.Status)
	require.Equal(t, ResourceReady, retrieved.Resources.Tmux.Status)
	require.True(t, tmux.HasSession("broken-session"))
}

func TestSessionService_RepairSession_StillFailing(t *testing.T) {
	service, sessionStore, _, tmux := setupSessionService(t)
	tmux.setCreateErr(fmt.Errorf("still broken"))

	session := &Session{
		ID:              "broken-session",
		Name:            "broken-session",
		TmuxSessionName: "broken-session",
		WorkspaceID:     "ws-1",
		Status:          StatusBroken,
		Resources: &Resources{
			Tmux: &ResourceState{Status: ResourceRemoved},
		},
		LastUsedAt: time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	result, err := service.RepairSession(ctx, "broken-session")
	require.NoError(t, err)
	require.Equal(t, StatusCreating, result.Status)

	waitForStatus(t, sessionStore, "broken-session", StatusBroken, 2*time.Second)

	retrieved, err := sessionStore.GetByID("broken-session")
	require.NoError(t, err)
	require.Equal(t, StatusBroken, retrieved.Status)
	require.Equal(t, ResourceFailed, retrieved.Resources.Tmux.Status)
	require.Contains(t, retrieved.Resources.Tmux.Error, "still broken")
}

func TestSessionService_RepairSession_AlreadyReady(t *testing.T) {
	service, sessionStore, _, tmux := setupSessionService(t)

	tmux.sessions["ok-session"] = true
	session := &Session{
		ID:              "ok-session",
		Name:            "ok-session",
		TmuxSessionName: "ok-session",
		WorkspaceID:     "ws-1",
		Status:          StatusBroken,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	result, err := service.RepairSession(ctx, "ok-session")
	require.NoError(t, err)
	require.Equal(t, StatusReady, result.Status)
}

func TestSessionService_RepairSession_NotBroken(t *testing.T) {
	service, sessionStore, _, tmux := setupSessionService(t)

	tmux.sessions["session-1"] = true
	session := &Session{
		ID:              "session-1",
		Name:            "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     "ws-1",
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	result, err := service.RepairSession(ctx, "session-1")
	require.NoError(t, err)
	require.Equal(t, StatusReady, result.Status)
}

func TestSessionService_Reconcile_MarksMissingTmuxBroken(t *testing.T) {
	service, sessionStore, _, _ := setupSessionService(t)

	session := &Session{
		ID:              "session-1",
		Name:            "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     "ws-1",
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	service.reconcileTmuxState(ctx)

	retrieved, err := sessionStore.GetByID("session-1")
	require.NoError(t, err)
	require.Equal(t, StatusBroken, retrieved.Status)
	require.Equal(t, ResourceRemoved, retrieved.Resources.Tmux.Status)
}

func TestSessionService_Reconcile_KeepsHealthyReady(t *testing.T) {
	service, sessionStore, _, tmux := setupSessionService(t)

	tmux.sessions["session-1"] = true
	session := &Session{
		ID:              "session-1",
		Name:            "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     "ws-1",
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	service.reconcileTmuxState(ctx)

	retrieved, err := sessionStore.GetByID("session-1")
	require.NoError(t, err)
	require.Equal(t, StatusReady, retrieved.Status)
}

func TestSessionService_Reconcile_SkipsDeleted(t *testing.T) {
	service, sessionStore, _, _ := setupSessionService(t)

	session := &Session{
		ID:              "deleted-1",
		Name:            "deleted-1",
		TmuxSessionName: "deleted-1",
		WorkspaceID:     "ws-1",
		Status:          StatusDeleted,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceRemoved}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	ctx := context.Background()
	service.reconcileTmuxState(ctx)

	retrieved, err := sessionStore.GetByID("deleted-1")
	require.NoError(t, err)
	require.Equal(t, StatusDeleted, retrieved.Status)
}
