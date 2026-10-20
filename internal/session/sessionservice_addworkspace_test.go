package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/mux"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestSessionService_AddWorkspace_RejectsDeletedSession(t *testing.T) {
	service, sessionStore, _, _, ws1ID, ws2ID := setupSessionService(t)

	session := &Session{Name: "deleted-session", Status: StatusDeleted, LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, swtStoreFor(service), session, ws1ID)

	ctx := context.Background()
	_, err := service.AddWorkspace(ctx, session.ID, WorkspaceBranchSpec{WorkspaceID: ws2ID, Branch: "main"})
	require.Error(t, err)
}

func TestSessionService_AddWorkspace_RejectsArchivedSession(t *testing.T) {
	service, sessionStore, _, _, ws1ID, ws2ID := setupSessionService(t)

	session := &Session{Name: "archived-session", Status: StatusArchived, LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, swtStoreFor(service), session, ws1ID)

	ctx := context.Background()
	_, err := service.AddWorkspace(ctx, session.ID, WorkspaceBranchSpec{WorkspaceID: ws2ID, Branch: "main"})
	require.Error(t, err)
}

func TestSessionService_AddWorkspace_RejectsNonGitWorkspace(t *testing.T) {
	service, sessionStore, _, _, ws1ID, ws2ID := setupSessionService(t)

	session := &Session{Name: "active-session", Status: StatusActive, SessionRoot: t.TempDir(), LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, swtStoreFor(service), session, ws1ID)

	ctx := context.Background()
	_, err := service.AddWorkspace(ctx, session.ID, WorkspaceBranchSpec{WorkspaceID: ws2ID, Branch: "main"})
	require.Error(t, err)
}

func TestSessionService_AddWorkspace_RejectsDuplicateWorkspace(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)

	session := &Session{Name: "active-session", Status: StatusActive, SessionRoot: t.TempDir(), LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, swtStoreFor(service), session, ws1ID)

	ws1 := &workspace.Workspace{}
	require.NoError(t, sessionStore.db.First(ws1, "id = ?", ws1ID).Error)
	ws1.IsGitRepo = true
	require.NoError(t, sessionStore.db.Save(ws1).Error)

	ctx := context.Background()
	_, err := service.AddWorkspace(ctx, session.ID, WorkspaceBranchSpec{WorkspaceID: ws1ID, Branch: "main"})
	require.Error(t, err)
}

func TestSessionService_AddWorkspace_CreatesJunctionRow(t *testing.T) {
	service, sessionStore, _, _, ws1ID, ws2ID := setupSessionService(t)

	session := &Session{Name: "active-session", Status: StatusActive, SessionRoot: t.TempDir(), LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, swtStoreFor(service), session, ws1ID)

	repo2 := &git.Repo{Path: "/tmp/other", FullName: "test/other"}
	require.NoError(t, sessionStore.db.Create(repo2).Error)
	ws2 := &workspace.Workspace{}
	require.NoError(t, sessionStore.db.First(ws2, "id = ?", ws2ID).Error)
	ws2.IsGitRepo = true
	ws2.RepoID = &repo2.ID
	require.NoError(t, sessionStore.db.Save(ws2).Error)

	ctx := context.Background()
	updated, err := service.AddWorkspace(ctx, session.ID, WorkspaceBranchSpec{WorkspaceID: ws2ID, Branch: "main"})
	require.NoError(t, err)
	require.Len(t, updated.Worktrees, 2)
	require.Equal(t, 1, updated.Worktrees[1].Position)
}

func TestSessionService_AddWorkspace_AssignsCollisionFreePath(t *testing.T) {
	service, sessionStore, _, _, ws1ID, ws2ID := setupSessionService(t)

	sessionRoot := t.TempDir()
	session := &Session{Name: "active-session", Status: StatusActive, SessionRoot: sessionRoot, LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, swtStoreFor(service), session, ws1ID)

	repo2 := &git.Repo{Path: "/tmp/other", FullName: "test/other"}
	require.NoError(t, sessionStore.db.Create(repo2).Error)
	ws2 := &workspace.Workspace{}
	require.NoError(t, sessionStore.db.First(ws2, "id = ?", ws2ID).Error)
	ws2.IsGitRepo = true
	ws2.RepoID = &repo2.ID
	require.NoError(t, sessionStore.db.Save(ws2).Error)

	ctx := context.Background()
	updated, err := service.AddWorkspace(ctx, session.ID, WorkspaceBranchSpec{WorkspaceID: ws2ID, Branch: "main"})
	require.NoError(t, err)
	require.Len(t, updated.Worktrees, 2)

	newWt := updated.Worktrees[1].Worktree
	require.NotNil(t, newWt)
	require.Equal(t, filepath.Join(sessionRoot, "other"), newWt.Path)
}

func setupTwoRepoSessionService(t *testing.T) (*SessionService, *SessionStore, *mux.FakeDriver, uint, uint) {
	t.Helper()

	repo1Path := initTestRepo(t)
	repo2Path := initTestRepo(t)

	database := setupTestDB(t)
	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")

	repo1 := &git.Repo{Path: repo1Path, FullName: "test/repo1"}
	require.NoError(t, database.Create(repo1).Error)
	repo2 := &git.Repo{Path: repo2Path, FullName: "test/repo2"}
	require.NoError(t, database.Create(repo2).Error)

	ws1 := &workspace.Workspace{Name: "repo1", Path: repo1Path, IsGitRepo: true, RepoID: &repo1.ID}
	require.NoError(t, workspaceStore.Add(ws1))
	ws2 := &workspace.Workspace{Name: "repo2", Path: repo2Path, IsGitRepo: true, RepoID: &repo2.ID}
	require.NoError(t, workspaceStore.Add(ws2))

	mock := mux.NewFakeDriver()
	muxService := createMuxService(t, database, mock, bus)
	gitService := git.NewGitService(database)
	workspaceService := workspace.NewWorkspaceService(workspaceStore, gitService)
	dismissedPRStore := NewDismissedPRStore(database)
	sessionActionStore := NewSessionActionStore(database)
	sessionWorktreeStore := NewSessionWorktreeStore(database)
	service := NewSessionService(sessionStore, sessionWorktreeStore, dismissedPRStore, sessionActionStore, NewSessionSetupStepStore(database), workspaceService, gitService, muxService, bus, "eqt/", t.TempDir(), t.TempDir())

	return service, sessionStore, mock, ws1.ID, ws2.ID
}

func TestSessionService_AddWorkspace_FullIntegration(t *testing.T) {
	service, sessionStore, _, ws1ID, ws2ID := setupTwoRepoSessionService(t)

	ctx := context.Background()
	sess, err := service.CreateSession(ctx, CreateSessionInput{
		Name:       "my-feature",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: ws1ID, BaseBranch: "main"}},
	})
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	updated, err := service.AddWorkspace(ctx, sess.ID, WorkspaceBranchSpec{WorkspaceID: ws2ID, BaseBranch: "main"})
	require.NoError(t, err)
	require.Len(t, updated.Worktrees, 2)

	expectedPath := filepath.Join(service.sessionsRoot, "my-feature", "repo2")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, statErr := os.Stat(expectedPath); statErr == nil {
			break
		}
		current, _ := sessionStore.GetByID(sess.ID)
		if current != nil && current.Status == StatusBroken {
			t.Fatalf("session became broken: %s", current.StatusError)
		}
		time.Sleep(25 * time.Millisecond)
	}

	info, err := os.Stat(expectedPath)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	waitForUpdatedAfter(t, sessionStore, sess.ID, updated.UpdatedAt, 10*time.Second)

	final, err := sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, final.Status)
}

func waitForUpdatedAfter(t *testing.T, store *SessionStore, id uint, since time.Time, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		sess, err := store.GetByID(id)
		if err == nil && sess.UpdatedAt.After(since) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("session %d was not updated after %v within %v", id, since, timeout)
}

func TestSessionService_AddWorkspace_RunsSetupActions(t *testing.T) {
	service, sessionStore, _, ws1ID, ws2ID := setupTwoRepoSessionService(t)

	ws2, err := service.workspaceService.GetWorkspace(context.Background(), ws2ID)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(ws2.Path, ".env.example"), []byte("A=1\n"), 0o644))
	pmScript := filepath.Join(t.TempDir(), "fake-pm")
	writeScript(t, pmScript, "#!/bin/sh\ntest -f .env && touch dep-installed\n")
	wrapper := filepath.Join(t.TempDir(), "wrapper")
	writeScript(t, wrapper, "#!/bin/sh\ntouch wrapped\nexec \"$@\"\n")
	writeWorkspaceConfig(t, ws2.Path, `{
		"setup_actions": [
			{"type": "install_deps", "package_manager": "`+pmScript+`", "run_with": ["`+wrapper+`"]},
			{"type": "copy_file", "src": ".env.example", "dest": ".env"}
		]
	}`)

	ctx := context.Background()
	sess, err := service.CreateSession(ctx, CreateSessionInput{
		Name:       "add-actions",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: ws1ID, BaseBranch: "main"}},
	})
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	updated, err := service.AddWorkspace(ctx, sess.ID, WorkspaceBranchSpec{WorkspaceID: ws2ID, BaseBranch: "main"})
	require.NoError(t, err)
	waitForUpdatedAfter(t, sessionStore, sess.ID, updated.UpdatedAt, 10*time.Second)

	final, err := sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.NotContains(t, final.StatusError, "copy_file")
	require.NotContains(t, final.StatusError, "install failed")

	wtDir := filepath.Join(service.sessionsRoot, "add-actions", "repo2")
	data, err := os.ReadFile(filepath.Join(wtDir, ".env"))
	require.NoError(t, err)
	require.Equal(t, "A=1\n", string(data))
	_, err = os.Stat(filepath.Join(wtDir, "dep-installed"))
	require.NoError(t, err, "install_deps should run after copy_file in the added worktree")
	_, err = os.Stat(filepath.Join(wtDir, "wrapped"))
	require.NoError(t, err, "install_deps should run through run_with")

	steps, err := service.setupStepStore.ListBySessionID(sess.ID)
	require.NoError(t, err)
	kinds := make([]SetupStepKind, len(steps))
	for i, st := range steps {
		kinds[i] = st.Kind
		require.Equal(t, SetupStepDone, st.Status, "step %q should be done", st.Label)
	}
	require.Equal(t, []SetupStepKind{
		StepKindCreateSessionDir,
		StepKindSetupWorktree,
		StepKindCopyFile,
		StepKindInstallDeps,
		StepKindApplyClaude,
	}, kinds, "add-workspace steps are persisted for progress tracking")
}
