package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	utmux "github.com/eleonorayaya/utena/internal/tmux"
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

func setupTwoRepoSessionService(t *testing.T) (*SessionService, *SessionStore, *utmux.MockRunner, uint, uint) {
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

	mock := utmux.NewMockRunner()
	tmuxService := createTmuxService(t, database, mock, bus)
	gitService := git.NewGitService(database)
	workspaceService := workspace.NewWorkspaceService(workspaceStore, gitService)
	dismissedPRStore := NewDismissedPRStore(database)
	sessionActionStore := NewSessionActionStore(database)
	sessionWorktreeStore := NewSessionWorktreeStore(database)
	service := NewSessionService(sessionStore, sessionWorktreeStore, dismissedPRStore, sessionActionStore, NewSessionSetupStepStore(database), workspaceService, gitService, tmuxService, bus, "eqt/", t.TempDir(), t.TempDir())

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

	final, err := sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, final.Status)
}
