package session

import (
	"context"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	utmux "github.com/eleonorayaya/utena/internal/tmux"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type prTestEnv struct {
	service          *SessionService
	sessionStore     *SessionStore
	dismissedPRStore *DismissedPRStore
	database         db.Database
	workspace        *workspace.Workspace
	repo             *git.Repo
	branch           *git.Branch
}

func setupPRTestEnv(t *testing.T) *prTestEnv {
	t.Helper()

	database, err := db.OpenInMemory()
	require.NoError(t, err)
	database.Migrate(
		&workspace.Workspace{},
		&git.Repo{},
		&git.Branch{},
		&git.Worktree{},
		&git.PullRequest{},
		&utmux.TmuxSession{},
		&Session{},
		&DismissedPR{},
		&claude.ClaudeSession{},
	)
	t.Cleanup(func() { database.Close() })

	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	dismissedPRStore := NewDismissedPRStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	gitService := git.NewGitService(database)

	repo := &git.Repo{Path: "/tmp/utena", FullName: "eleonorayaya/utena"}
	require.NoError(t, database.Create(repo).Error)

	repoID := repo.ID
	ws := &workspace.Workspace{Name: "utena", Path: "/tmp/utena", IsGitRepo: true, RepoID: &repoID}
	require.NoError(t, workspaceStore.Add(ws))

	branch := &git.Branch{Name: "feature-pr", RepoID: repo.ID, ExistsLocal: false, ExistsRemote: true}
	require.NoError(t, database.Create(branch).Error)

	service := NewSessionService(sessionStore, dismissedPRStore, workspaceService, gitService, nil, bus, "eqt/", t.TempDir())

	return &prTestEnv{
		service:          service,
		sessionStore:     sessionStore,
		dismissedPRStore: dismissedPRStore,
		database:         database,
		workspace:        ws,
		repo:             repo,
		branch:           branch,
	}
}

func TestHandlePRDiscovered_CreatesSession(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	event := eventbus.Event{
		Type: git.EventPRDiscovered,
		Data: git.PRDiscoveredEvent{
			PullRequest: &git.PullRequest{
				RepoID:         env.repo.ID,
				Number:         42,
				HeadBranchID:   &branchID,
				Title:          "Test PR",
				State:          git.PRStateOpen,
				IsAssignedToMe: true,
			},
			Repo: env.repo,
		},
	}

	err := env.service.handlePRDiscovered(ctx, event)
	require.NoError(t, err)

	sess, err := env.sessionStore.GetByBranchID(branchID)
	require.NoError(t, err)
	require.Equal(t, StatusPending, sess.Status)
	require.Equal(t, env.workspace.ID, sess.WorkspaceID)
	require.Equal(t, "feature-pr", sess.Name)
}

func TestHandlePRDiscovered_SkipsUnassigned(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	event := eventbus.Event{
		Type: git.EventPRDiscovered,
		Data: git.PRDiscoveredEvent{
			PullRequest: &git.PullRequest{
				RepoID:         env.repo.ID,
				Number:         42,
				HeadBranchID:   &branchID,
				Title:          "Test PR",
				State:          git.PRStateOpen,
				IsAssignedToMe: false,
			},
			Repo: env.repo,
		},
	}

	err := env.service.handlePRDiscovered(ctx, event)
	require.NoError(t, err)

	_, err = env.sessionStore.GetByBranchID(branchID)
	require.Error(t, err)
}

func TestHandlePRDiscovered_SkipsExistingSession(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	existing := &Session{
		Name:        "feature-pr",
		WorkspaceID: env.workspace.ID,
		BranchID:    &branchID,
		Status:      StatusActive,
		LastUsedAt:  time.Now(),
	}
	require.NoError(t, env.sessionStore.Add(existing))

	event := eventbus.Event{
		Type: git.EventPRDiscovered,
		Data: git.PRDiscoveredEvent{
			PullRequest: &git.PullRequest{
				RepoID:         env.repo.ID,
				Number:         42,
				HeadBranchID:   &branchID,
				Title:          "Test PR",
				State:          git.PRStateOpen,
				IsAssignedToMe: true,
			},
			Repo: env.repo,
		},
	}

	err := env.service.handlePRDiscovered(ctx, event)
	require.NoError(t, err)

	sessions, err := env.sessionStore.List()
	require.NoError(t, err)
	require.Len(t, sessions, 1)
}

func TestHandlePRStateChanged_CompletesSessionOnMerge(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	sess := &Session{
		Name:        "feature-pr",
		WorkspaceID: env.workspace.ID,
		BranchID:    &branchID,
		Status:      StatusActive,
		LastUsedAt:  time.Now(),
	}
	require.NoError(t, env.sessionStore.Add(sess))

	event := eventbus.Event{
		Type: git.EventPRStateChanged,
		Data: git.PRStateChangedEvent{
			PullRequest: &git.PullRequest{
				Number:       42,
				HeadBranchID: &branchID,
			},
			OldState: git.PRStateOpen,
			NewState: git.PRStateMerged,
		},
	}

	err := env.service.handlePRStateChanged(ctx, event)
	require.NoError(t, err)

	updated, err := env.sessionStore.GetByBranchID(branchID)
	require.NoError(t, err)
	require.Equal(t, StatusCompleted, updated.Status)
}

func TestHandlePRStateChanged_IgnoresNonMerge(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	sess := &Session{
		Name:        "feature-pr",
		WorkspaceID: env.workspace.ID,
		BranchID:    &branchID,
		Status:      StatusActive,
		LastUsedAt:  time.Now(),
	}
	require.NoError(t, env.sessionStore.Add(sess))

	event := eventbus.Event{
		Type: git.EventPRStateChanged,
		Data: git.PRStateChangedEvent{
			PullRequest: &git.PullRequest{
				Number:       42,
				HeadBranchID: &branchID,
			},
			OldState: git.PRStateOpen,
			NewState: git.PRStateClosed,
		},
	}

	err := env.service.handlePRStateChanged(ctx, event)
	require.NoError(t, err)

	updated, err := env.sessionStore.GetByBranchID(branchID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, updated.Status)
}

func TestHandlePRStateChanged_NoSessionForBranch(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	event := eventbus.Event{
		Type: git.EventPRStateChanged,
		Data: git.PRStateChangedEvent{
			PullRequest: &git.PullRequest{
				Number:       42,
				HeadBranchID: &branchID,
			},
			OldState: git.PRStateOpen,
			NewState: git.PRStateMerged,
		},
	}

	err := env.service.handlePRStateChanged(ctx, event)
	require.NoError(t, err)
}

func TestCompletedCleanupTask_ArchivesStale(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	sess := &Session{
		Name:        "stale-session",
		WorkspaceID: env.workspace.ID,
		BranchID:    &branchID,
		Status:      StatusCompleted,
		LastUsedAt:  time.Now().Add(-10 * time.Minute),
	}
	require.NoError(t, env.sessionStore.Add(sess))

	task := &completedCleanupTask{service: env.service}
	err := task.Run(ctx)
	require.NoError(t, err)

	updated, err := env.sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.Equal(t, StatusArchived, updated.Status)
}

func TestCompletedCleanupTask_SkipsAttached(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	sess := &Session{
		Name:        "attached-session",
		WorkspaceID: env.workspace.ID,
		BranchID:    &branchID,
		Status:      StatusCompleted,
		IsAttached:  true,
		LastUsedAt:  time.Now().Add(-10 * time.Minute),
	}
	require.NoError(t, env.sessionStore.Add(sess))

	task := &completedCleanupTask{service: env.service}
	err := task.Run(ctx)
	require.NoError(t, err)

	updated, err := env.sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.Equal(t, StatusCompleted, updated.Status)
}

func TestCompletedCleanupTask_SkipsRecent(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	sess := &Session{
		Name:        "recent-session",
		WorkspaceID: env.workspace.ID,
		BranchID:    &branchID,
		Status:      StatusCompleted,
		LastUsedAt:  time.Now().Add(-1 * time.Minute),
	}
	require.NoError(t, env.sessionStore.Add(sess))

	task := &completedCleanupTask{service: env.service}
	err := task.Run(ctx)
	require.NoError(t, err)

	updated, err := env.sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.Equal(t, StatusCompleted, updated.Status)
}

func TestHandlePRDiscovered_SkipsDismissed(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	pr := &git.PullRequest{
		RepoID:         env.repo.ID,
		Number:         42,
		HeadBranchID:   &env.branch.ID,
		Title:          "Test PR",
		State:          git.PRStateOpen,
		IsAssignedToMe: true,
	}
	require.NoError(t, env.database.Create(pr).Error)

	require.NoError(t, env.dismissedPRStore.Add(&DismissedPR{
		PullRequestID: pr.ID,
		DismissedAt:   time.Now(),
	}))

	event := eventbus.Event{
		Type: git.EventPRDiscovered,
		Data: git.PRDiscoveredEvent{
			PullRequest: pr,
			Repo:        env.repo,
		},
	}

	err := env.service.handlePRDiscovered(ctx, event)
	require.NoError(t, err)

	_, err = env.sessionStore.GetByBranchID(env.branch.ID)
	require.Error(t, err)
}
