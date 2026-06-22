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
	require.NoError(t, workspaceStore.Add(ws1))
	require.NoError(t, workspaceStore.Add(ws2))

	mock := utmux.NewMockRunner()
	tmuxService := createTmuxService(t, database, mock, bus)
	gitService := git.NewGitService(database)
	workspaceService := workspace.NewWorkspaceService(workspaceStore, gitService)
	dismissedPRStore := NewDismissedPRStore(database)
	sessionActionStore := NewSessionActionStore(database)
	sessionWorktreeStore := NewSessionWorktreeStore(database)
	configDir := t.TempDir()
	sessionsRoot := t.TempDir()
	service := NewSessionService(sessionStore, sessionWorktreeStore, dismissedPRStore, sessionActionStore, NewSessionSetupStepStore(database), workspaceService, gitService, tmuxService, bus, "eqt/", configDir, sessionsRoot)
	return service, sessionStore, workspaceStore, mock, ws1.ID, ws2.ID
}

func swtStoreFor(svc *SessionService) *SessionWorktreeStore {
	return svc.sessionWorktreeStore
}

// createSessionLegacy wraps the new CreateSession API in the shape that
// existing tests were written for: a pre-built Session struct mutated in
// place, plus a single workspace + branch/baseBranch pair.
func createSessionLegacy(ctx context.Context, service *SessionService, sess *Session, wsID uint, branch, baseBranch string, actions ...*SessionAction) error {
	spec := WorkspaceBranchSpec{WorkspaceID: wsID, Branch: branch, BaseBranch: baseBranch}
	name := sess.Name
	if name == "" && branch != "" {
		name = service.nameFromBranch(branch)
	}
	created, err := service.CreateSession(ctx, CreateSessionInput{
		Name:       name,
		Workspaces: []WorkspaceBranchSpec{spec},
		TodoID:     sess.TodoID,
		Actions:    actions,
	})
	if err != nil {
		return err
	}
	if created != nil {
		*sess = *created
	}
	return nil
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

func TestSessionService_OnAppStart_MarksLegacyLayoutSessionsBroken(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)
	ctx := context.Background()

	legacy := &Session{
		Name:        "legacy-one",
		Status:      StatusActive,
		SessionRoot: "/tmp/some-legacy-path/legacy-one",
		LastUsedAt:  time.Now(),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), legacy, ws1ID)

	container := &Session{
		Name:        "container-one",
		Status:      StatusActive,
		SessionRoot: filepath.Join(service.sessionsRoot, "container-one"),
		LastUsedAt:  time.Now(),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), container, ws1ID)

	require.NoError(t, service.OnAppStart(ctx))

	loadedLegacy, err := sessionStore.GetByID(legacy.ID)
	require.NoError(t, err)
	require.Equal(t, StatusBroken, loadedLegacy.Status)
	require.Contains(t, loadedLegacy.StatusError, "legacy layout")

	loadedContainer, err := sessionStore.GetByID(container.ID)
	require.NoError(t, err)
	require.NotEqual(t, StatusBroken, loadedContainer.Status, "container-layout session should not be flagged")
}

func TestSessionService_Reconcile_RecoversBrokenSessionOnceGitHealthy(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)
	ctx := context.Background()

	sess := &Session{
		Name:        "previously-broken",
		Status:      StatusBroken,
		StatusError: gitUnhealthyError,
		LastUsedAt:  time.Now(),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), sess, ws1ID)

	require.NoError(t, service.OnAppStart(ctx))

	loaded, err := sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.NotEqual(t, StatusBroken, loaded.Status, "broken-by-git-mismatch session should auto-revive once worktree is healthy again")
	require.Empty(t, loaded.StatusError)
}

func TestSessionService_Reconcile_LeavesOrphanedBrokenSessionAlone(t *testing.T) {
	service, sessionStore, _, _, _, _ := setupSessionService(t)
	ctx := context.Background()

	orphan := &Session{
		Name:        "orphan-broken",
		Status:      StatusBroken,
		StatusError: orphanedSessionError,
		LastUsedAt:  time.Now(),
	}
	require.NoError(t, sessionStore.Add(orphan))

	require.NoError(t, service.OnAppStart(ctx))

	loaded, err := sessionStore.GetByID(orphan.ID)
	require.NoError(t, err)
	require.Equal(t, StatusBroken, loaded.Status, "orphan-marked broken sessions must not auto-revive")
	require.Contains(t, loaded.StatusError, "predates")
}

func TestSessionService_OnAppStart_RecoversStuckCreatingSessions(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)
	ctx := context.Background()

	stuck := &Session{
		Name:       "stuck-session",
		Status:     StatusCreating,
		LastUsedAt: time.Now().Add(-1 * time.Hour),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), stuck, ws1ID)

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
	swStore := swtStoreFor(service)

	now := time.Now()
	session1 := &Session{Name: "session-1", Status: StatusActive, LastUsedAt: now.Add(-1 * time.Hour)}
	session2 := &Session{Name: "session-2", Status: StatusActive, LastUsedAt: now}
	addTestSession(t, sessionStore, swStore, session1, ws1ID)
	addTestSession(t, sessionStore, swStore, session2, ws2ID)

	ctx := context.Background()
	sessions, err := service.ListSessions(ctx)
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	require.Equal(t, "session-2", sessions[0].Name, "Most recent session should be first")
}

func TestSessionService_ListSessionsByWorkspace(t *testing.T) {
	service, sessionStore, _, _, ws1ID, ws2ID := setupSessionService(t)
	swStore := swtStoreFor(service)

	now := time.Now()
	session1 := &Session{Name: "session-1", Status: StatusActive, LastUsedAt: now.Add(-1 * time.Hour)}
	session2 := &Session{Name: "session-2", Status: StatusActive, LastUsedAt: now}
	session3 := &Session{Name: "session-3", Status: StatusActive, LastUsedAt: now}
	addTestSession(t, sessionStore, swStore, session1, ws1ID)
	addTestSession(t, sessionStore, swStore, session2, ws2ID)
	addTestSession(t, sessionStore, swStore, session3, ws1ID)

	ctx := context.Background()
	sessions, err := service.ListSessionsByWorkspace(ctx, ws1ID)
	require.NoError(t, err)
	require.Len(t, sessions, 2)

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
		Name:       "session-1",
		IsAttached: true,
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), session, ws1ID)

	ctx := context.Background()
	retrieved, err := service.GetSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, session.ID, retrieved.ID)
	require.Equal(t, session.IsAttached, retrieved.IsAttached)
}

func TestSessionService_GetSession_NotFound(t *testing.T) {
	service, _, _, _, _, _ := setupSessionService(t)

	ctx := context.Background()
	_, err := service.GetSession(ctx, 99999)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionService_CreateSession_RejectsNoBranch(t *testing.T) {
	repoPath := initTestRepo(t)
	service, _, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	session := &Session{Name: "session-1"}

	ctx := context.Background()
	err := createSessionLegacy(ctx, service, session, wsGitID, "", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "branch")
}

func TestSessionService_CreateSession_RejectsNonGitWorkspace(t *testing.T) {
	service, _, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{Name: "session-1"}

	ctx := context.Background()
	err := createSessionLegacy(ctx, service, session, ws1ID, "main", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a git repository")
}

func TestSessionService_CreateSession_InvalidWorkspace(t *testing.T) {
	service, _, _, _, _, _ := setupSessionService(t)

	session := &Session{
		Name:       "session-1",
		LastUsedAt: time.Now(),
	}

	ctx := context.Background()
	err := createSessionLegacy(ctx, service, session, 99999, "main", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionService_UpdateSession(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:       "session-1",
		IsAttached: false,
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), session, ws1ID)

	session.IsAttached = true
	ctx := context.Background()
	err := service.UpdateSession(ctx, session)
	require.NoError(t, err)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.True(t, retrieved.IsAttached)
}

func TestSessionService_DeleteSession_RemovesWorktreeDirOnDisk(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	session := &Session{Name: "drop-me"}
	ctx := context.Background()
	require.NoError(t, createSessionLegacy(ctx, service, session, wsGitID, "", "main"))
	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)

	sessionRoot := filepath.Join(service.sessionsRoot, "drop-me")
	worktreePath := filepath.Join(sessionRoot, "git-repo")
	info, err := os.Stat(worktreePath)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	archived, err := service.ArchiveSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusArchived, archived.Status)

	require.NoError(t, service.DeleteSession(ctx, session.ID, true, false))

	_, err = os.Stat(worktreePath)
	require.True(t, os.IsNotExist(err), "worktree dir must be removed by DeleteSession")
	_, err = os.Stat(sessionRoot)
	require.True(t, os.IsNotExist(err), "session root dir must be removed by DeleteSession")
}

func TestSessionService_DeleteSession_HardDeletesArchived(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)
	swt := swtStoreFor(service)

	session := &Session{
		Name:       "session-1",
		Status:     StatusArchived,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swt, session, ws1ID)

	rows, err := swt.ListBySessionID(session.ID)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	wtID := rows[0].WorktreeID

	ctx := context.Background()
	require.NoError(t, service.DeleteSession(ctx, session.ID, true, false))

	_, err = sessionStore.GetByID(session.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")

	remaining, err := swt.ListBySessionID(session.ID)
	require.NoError(t, err)
	require.Empty(t, remaining, "session_worktree join rows must be cleaned up")

	_, err = service.gitService.GetWorktree(wtID)
	require.Error(t, err, "worktree record must be cleaned up")
}

func TestSessionService_DeleteSession_RejectsNonArchived(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:       "still-active",
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), session, ws1ID)

	ctx := context.Background()
	err := service.DeleteSession(ctx, session.ID, true, false)
	require.ErrorIs(t, err, ErrSessionNotArchived)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, retrieved.Status)
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
		Name:       "stuck-session",
		Status:     StatusCreating,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), sess, ws1ID)

	ctx := context.Background()
	err := service.DeleteSession(ctx, sess.ID, true, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot delete session while it is being created")
}

func TestSessionService_DeleteSession_Creating_Force(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	sess := &Session{
		Name:       "stuck-session",
		Status:     StatusCreating,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), sess, ws1ID)

	ctx := context.Background()
	err := service.DeleteSession(ctx, sess.ID, true, true)
	require.NoError(t, err)

	_, err = sessionStore.GetByID(sess.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionService_ArchiveSession_AllowsBroken(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	sess := &Session{
		Name:       "broken-session",
		Status:     StatusBroken,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), sess, ws1ID)

	ctx := context.Background()
	archived, err := service.ArchiveSession(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, StatusArchived, archived.Status)
}

func TestSessionService_ArchiveSession_RejectsCreating(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	sess := &Session{
		Name:       "creating-session",
		Status:     StatusCreating,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), sess, ws1ID)

	ctx := context.Background()
	_, err := service.ArchiveSession(ctx, sess.ID)
	require.Error(t, err)
}

func TestSessionService_DetachWorktreesByRepoID(t *testing.T) {
	service, sessionStore, workspaceStore, _, ws1ID, ws2ID := setupSessionService(t)
	swStore := swtStoreFor(service)
	database := service.store.db

	soloIn1 := &Session{Name: "solo-1", Status: StatusActive, LastUsedAt: time.Now()}
	soloIn2 := &Session{Name: "solo-2", Status: StatusActive, LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, swStore, soloIn1, ws1ID)
	addTestSession(t, sessionStore, swStore, soloIn2, ws2ID)

	multi := &Session{Name: "multi", Status: StatusActive, LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, swStore, multi, ws1ID)
	attachTestWorktree(t, database, swStore, multi.ID, ws2ID, 1)

	ws1, err := workspaceStore.GetByID(ws1ID)
	require.NoError(t, err)
	require.NotNil(t, ws1.RepoID)
	repo1ID := *ws1.RepoID

	ctx := context.Background()
	require.NoError(t, service.DetachWorktreesByRepoID(ctx, repo1ID))

	var wtCount int64
	require.NoError(t, database.Model(&git.Worktree{}).Where("repo_id = ?", repo1ID).Count(&wtCount).Error)
	require.Zero(t, wtCount, "all worktrees in the migrated repo should be deleted")

	soloIn1Reloaded, err := sessionStore.GetByID(soloIn1.ID)
	require.NoError(t, err)
	require.Empty(t, soloIn1Reloaded.Worktrees, "solo session in migrated repo should have no worktrees left")

	soloIn2Reloaded, err := sessionStore.GetByID(soloIn2.ID)
	require.NoError(t, err)
	require.Len(t, soloIn2Reloaded.Worktrees, 1, "session in untouched repo must keep its worktree")

	multiReloaded, err := sessionStore.GetByID(multi.ID)
	require.NoError(t, err)
	require.Len(t, multiReloaded.Worktrees, 1, "multi-repo session should keep its remaining worktree")
	require.NotNil(t, multiReloaded.Worktrees[0].Worktree)
	require.NotEqual(t, repo1ID, multiReloaded.Worktrees[0].Worktree.RepoID)
}

func TestSessionService_DetachWorktreesByRepoID_NoRows(t *testing.T) {
	service, _, _, _, _, _ := setupSessionService(t)
	require.NoError(t, service.DetachWorktreesByRepoID(context.Background(), 99999))
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	bareDir := t.TempDir()
	t.Cleanup(func() {
		filepath.Walk(bareDir, func(path string, _ os.FileInfo, err error) error {
			if err == nil {
				os.Chmod(path, 0700)
			}
			return nil
		})
	})
	dir := t.TempDir()
	gitEnv := append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)

	bareCmds := [][]string{
		{"git", "init", "--bare", "-b", "main"},
	}
	for _, args := range bareCmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = bareDir
		cmd.Env = gitEnv
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v failed: %s", args, string(out))
	}

	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "config", "commit.gpgsign", "false"},
		{"git", "config", "tag.gpgsign", "false"},
		{"git", "remote", "add", "origin", bareDir},
		{"git", "commit", "--allow-empty", "-m", "init"},
		{"git", "push", "-u", "origin", "HEAD"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = gitEnv
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

	repo := &git.Repo{Path: repoPath, FullName: "test/git-repo"}
	require.NoError(t, database.Create(repo).Error)
	repoID := repo.ID
	wsGit := &workspace.Workspace{Name: "git-repo", Path: repoPath, IsGitRepo: true, RepoID: &repoID}
	require.NoError(t, workspaceStore.Add(wsGit))

	mock := utmux.NewMockRunner()
	tmuxService := createTmuxService(t, database, mock, bus)
	gitService := git.NewGitService(database)
	workspaceService := workspace.NewWorkspaceService(workspaceStore, gitService)
	dismissedPRStore := NewDismissedPRStore(database)
	sessionActionStore := NewSessionActionStore(database)
	sessionWorktreeStore := NewSessionWorktreeStore(database)
	service := NewSessionService(sessionStore, sessionWorktreeStore, dismissedPRStore, sessionActionStore, NewSessionSetupStepStore(database), workspaceService, gitService, tmuxService, bus, "eqt/", configDir, t.TempDir())
	return service, sessionStore, mock, wsGit.ID, database, gitService
}

func TestSessionService_CreateSession_WithWorktree(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, mock, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	session := &Session{
		Name: "my-feature",
	}

	ctx := context.Background()
	err := createSessionLegacy(ctx, service, session, wsGitID, "", "main")
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)

	expectedPath := filepath.Join(service.sessionsRoot, "my-feature", "git-repo")

	info, err := os.Stat(expectedPath)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	require.True(t, mock.HasSessionByName("my-feature"))

	finalSess, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.NotNil(t, finalSess.TmuxSessionID)
	require.NotNil(t, finalSess.TmuxSession)
	require.Equal(t, "my-feature", finalSess.TmuxSession.Name)
	require.Equal(t, utmux.TmuxStatusActive, finalSess.TmuxSession.Status)
}

func TestSessionService_CreateSession_EagerlyRegistersPendingTmuxRecord(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	prev := service.setupTimeout
	service.setupTimeout = 1 * time.Nanosecond
	defer func() { service.setupTimeout = prev }()

	session := &Session{
		Name: "eager",
	}

	ctx := context.Background()
	require.NoError(t, createSessionLegacy(ctx, service, session, wsGitID, "main", ""))

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.NotNil(t, retrieved.TmuxSessionID, "TmuxSessionID should be linked synchronously by CreateSession")
	require.NotNil(t, retrieved.TmuxSession)
	require.Equal(t, "eager", retrieved.TmuxSession.Name)
}

func TestSessionService_CreateSession_TimesOut(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())
	service.setupTimeout = 1 * time.Nanosecond

	session := &Session{
		Name: "slow-feature",
	}

	ctx := context.Background()
	err := createSessionLegacy(ctx, service, session, wsGitID, "", "main")
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
		Name: "my-feature",
	}

	ctx := context.Background()
	err := createSessionLegacy(ctx, service, session, wsGitID, "", "nonexistent")
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusBroken, 5*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusBroken, retrieved.Status)
	require.Contains(t, retrieved.StatusError, "branch")
	require.False(t, mock.HasSessionByName("my-feature"))
}

func TestSessionService_ActivateSession_TouchesWorkspace(t *testing.T) {
	service, sessionStore, workspaceStore, tmux, ws1ID, _ := setupSessionService(t)

	tmux.Sessions["utena-session-1"] = true
	session := &Session{
		Name:       "session-1",
		Status:     StatusActive,
		LastUsedAt: time.Now().Add(-1 * time.Hour),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), session, ws1ID)

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
		Name:       "broken-session",
		Status:     StatusBroken,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), session, ws1ID)

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

	service, sessionStore, mock, wsGitID, gitDB, gitService := setupWorktreeSessionServiceFull(t, repoPath, t.TempDir())

	repo, err := gitService.GetRepoByPath(repoPath)
	require.NoError(t, err)
	branch := &git.Branch{Name: branchName, RepoID: repo.ID, ExistsLocal: false, ExistsRemote: true}
	require.NoError(t, gitDB.Create(branch).Error)

	worktreePathPinned := filepath.Join(repoPath, ".worktrees", "harleyk--catalog-docs")
	pending := &Session{
		Name:        branchName,
		Status:      StatusPending,
		SessionRoot: worktreePathPinned,
		LastUsedAt:  time.Now(),
	}
	require.NoError(t, sessionStore.Add(pending))
	_ = wsGitID
	wt := &git.Worktree{
		Path:     worktreePathPinned,
		BranchID: branch.ID,
		RepoID:   repo.ID,
		Status:   git.WorktreeStatusPending,
	}
	require.NoError(t, gitDB.Create(wt).Error)
	require.NoError(t, service.sessionWorktreeStore.Add(&SessionWorktree{SessionID: pending.ID, WorktreeID: wt.ID, Position: 0}))
	tmuxRecord, err := service.tmuxService.RegisterPending(fmt.Sprintf("git-repo-%s", branchName), worktreePathPinned, nil)
	require.NoError(t, err)
	pending.TmuxSessionID = &tmuxRecord.ID
	require.NoError(t, sessionStore.Update(pending))

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
		Name:       "session-1",
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), session, ws1ID)
	tmuxRecord, err := service.tmuxService.RegisterPending("utena-session-1", "/tmp/utena", nil)
	require.NoError(t, err)
	session.TmuxSessionID = &tmuxRecord.ID
	require.NoError(t, sessionStore.Update(session))

	ctx := context.Background()
	result, err := service.ActivateSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, result.Status)
	require.True(t, result.IsAttached)
	require.True(t, tmux.HasSessionByName("utena-session-1"))
}

func TestSessionService_RefreshSession_DetectsMissingTmux(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	tmux.Sessions["utena-session-1"] = true
	session := &Session{
		Name:       "session-1",
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), session, ws1ID)

	ctx := context.Background()
	tmux.RemoveSession("utena-session-1")
	require.NoError(t, service.tmuxService.HandleSessionClosed(ctx, "utena-session-1"))

	refreshed, err := service.RefreshSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusInactive, refreshed.Status)
}

func TestSessionService_RefreshSession_AllHealthy(t *testing.T) {
	database := setupTestDB(t)
	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
	ws := &workspace.Workspace{Name: "utena", Path: "/tmp/utena", IsGitRepo: true}
	require.NoError(t, workspaceStore.Add(ws))
	mock := utmux.NewMockRunner()
	tmuxService := createTmuxService(t, database, mock, bus)
	gitService := git.NewGitService(database)
	workspaceService := workspace.NewWorkspaceService(workspaceStore, gitService)
	swtStore := NewSessionWorktreeStore(database)
	service := NewSessionService(sessionStore, swtStore, NewDismissedPRStore(database), NewSessionActionStore(database), NewSessionSetupStepStore(database), workspaceService, gitService, tmuxService, bus, "eqt/", t.TempDir(), t.TempDir())

	ts := &utmux.TmuxSession{Name: "utena-session-1", StartDir: "/tmp/utena", Status: utmux.TmuxStatusActive}
	require.NoError(t, database.Create(ts).Error)
	session := &Session{
		Name:          "session-1",
		Status:        StatusActive,
		TmuxSessionID: &ts.ID,
		LastUsedAt:    time.Now(),
	}
	addTestSession(t, sessionStore, swtStore, session, ws.ID)

	ctx := context.Background()
	refreshed, err := service.RefreshSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, refreshed.Status)
}

func TestSessionService_RepairSession_NotBroken(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	tmux.Sessions["utena-session-1"] = true
	session := &Session{
		Name:       "session-1",
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), session, ws1ID)

	ctx := context.Background()
	_, err := service.RepairSession(ctx, session.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrSessionNotBroken)
}

func TestSessionService_Reconcile_MarksMissingTmuxBroken(t *testing.T) {
	service, sessionStore, _, tmux, ws1ID, _ := setupSessionService(t)

	tmux.Sessions["utena-session-1"] = true
	session := &Session{
		Name:       "session-1",
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), session, ws1ID)

	ctx := context.Background()
	tmux.RemoveSession("utena-session-1")
	require.NoError(t, service.tmuxService.HandleSessionClosed(ctx, "utena-session-1"))

	sessions, err := sessionStore.List()
	require.NoError(t, err)
	service.reconcileTmuxState(ctx, sessions)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusInactive, retrieved.Status)
}

func TestSessionService_Reconcile_SkipsDeleted(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{
		Name:       "deleted-1",
		Status:     StatusDeleted,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swtStoreFor(service), session, ws1ID)

	ctx := context.Background()
	sessions, err := sessionStore.List()
	require.NoError(t, err)
	service.reconcileTmuxState(ctx, sessions)

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
		Name: "init-test",
	}

	ctx := context.Background()
	err := createSessionLegacy(ctx, service, session, wsGitID, "", "main")
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
		Name: "repo-init",
	}

	ctx := context.Background()
	err := createSessionLegacy(ctx, service, session, wsGitID, "", "main")
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)

	data, err := os.ReadFile(markerFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "git-repo")
}

func TestSessionService_WorktreeInit_FailingScriptContinues(t *testing.T) {
	repoPath := initTestRepo(t)
	configDir := t.TempDir()

	writeScript(t, filepath.Join(configDir, "worktree-setup"), "#!/bin/sh\nexit 1\n")

	service, sessionStore, mock, wsGitID := setupWorktreeSessionService(t, repoPath, configDir)

	session := &Session{
		Name: "fail-init",
	}

	ctx := context.Background()
	err := createSessionLegacy(ctx, service, session, wsGitID, "", "main")
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, retrieved.Status)
	require.True(t, mock.HasSessionByName("fail-init"))
}

func TestSessionService_WorktreeInit_MissingScriptsSkipped(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	session := &Session{
		Name: "no-scripts",
	}

	ctx := context.Background()
	err := createSessionLegacy(ctx, service, session, wsGitID, "", "main")
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

	writeScript(t, filepath.Join(repoPath, ".utena", "worktree-setup"), fmt.Sprintf("#!/bin/sh\npwd > %s\n", markerFile))

	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, configDir)

	session := &Session{
		Name: "wd-test",
	}

	ctx := context.Background()
	err := createSessionLegacy(ctx, service, session, wsGitID, "", "main")
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)

	data, err := os.ReadFile(markerFile)
	require.NoError(t, err)

	expectedPath := filepath.Join(service.sessionsRoot, "wd-test", "git-repo")
	require.Contains(t, string(data), expectedPath)
}

func TestSessionService_CreateSession_DirtyMainRepo_SetsWarning(t *testing.T) {
	repoPath := initTestRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "dirty.txt"), []byte("uncommitted"), 0644))

	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	sess := &Session{Name: "warn-feature"}
	ctx := context.Background()
	require.NoError(t, createSessionLegacy(ctx, service, sess, wsGitID, "", "main"))

	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	retrieved, err := sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, retrieved.Status)
	require.Contains(t, retrieved.StatusError, "branch not pulled")
}

func TestSessionService_CreateSession_CleanRepo_NoWarning(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	sess := &Session{Name: "clean-feature"}
	ctx := context.Background()
	require.NoError(t, createSessionLegacy(ctx, service, sess, wsGitID, "", "main"))

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

	sess := &Session{Name: "warn-repair"}
	ctx := context.Background()
	require.NoError(t, createSessionLegacy(ctx, service, sess, wsGitID, "", "main"))
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
	require.NoError(t, gitService.MigrateToBare(ctx, repoPath, nil))

	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
	repo := &git.Repo{Path: repoPath, FullName: "test/git-repo"}
	require.NoError(t, database.Create(repo).Error)
	repoID := repo.ID
	wsGit := &workspace.Workspace{Name: "git-repo", Path: repoPath, IsGitRepo: true, IsBare: true, RepoID: &repoID}
	require.NoError(t, workspaceStore.Add(wsGit))

	mock := utmux.NewMockRunner()
	tmuxService := createTmuxService(t, database, mock, bus)
	workspaceService := workspace.NewWorkspaceService(workspaceStore, gitService)
	dismissedPRStore := NewDismissedPRStore(database)
	sessionActionStore := NewSessionActionStore(database)
	sessionWorktreeStore := NewSessionWorktreeStore(database)
	service := NewSessionService(sessionStore, sessionWorktreeStore, dismissedPRStore, sessionActionStore, NewSessionSetupStepStore(database), workspaceService, gitService, tmuxService, bus, "eqt/", configDir, t.TempDir())
	return service, sessionStore, mock, wsGit.ID, repoPath
}

func TestRunSetup_BareWorkspace_WritesContainerClaudeSettings(t *testing.T) {
	service, sessionStore, _, wsID, _ := setupBareWorktreeSessionService(t, t.TempDir())

	session := &Session{
		Name: "my-feature",
	}

	ctx := context.Background()
	err := createSessionLegacy(ctx, service, session, wsID, "", "main")
	require.NoError(t, err)

	waitForStatus(t, sessionStore, session.ID, StatusActive, 5*time.Second)

	sessionRoot := filepath.Join(service.sessionsRoot, "my-feature")
	settings := filepath.Join(sessionRoot, ".claude", "settings.local.json")
	data, err := os.ReadFile(settings)
	require.NoError(t, err, "session-root settings.local.json should exist")
	require.NotEmpty(t, data)

	guide := filepath.Join(sessionRoot, "CLAUDE.md")
	_, err = os.Stat(guide)
	require.NoError(t, err, "session-root CLAUDE.md should exist")
}
