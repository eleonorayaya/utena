package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/db/testdb"
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
	swtStore         *SessionWorktreeStore
	dismissedPRStore *DismissedPRStore
	database         db.Database
	workspace        *workspace.Workspace
	repo             *git.Repo
	branch           *git.Branch
	wtSeq            int
}

// attachBranchWorktree creates a Worktree row tied to the env's workspace+repo
// and the supplied branch, then links it to the given session via a fresh
// SessionWorktree row.
func (env *prTestEnv) attachBranchWorktree(t *testing.T, sessionID uint, branchID uint, position int) *git.Worktree {
	t.Helper()
	env.wtSeq++
	wt := &git.Worktree{
		Path:     filepath.Join(env.workspace.Path, ".worktrees", fmt.Sprintf("pr-test-%d", env.wtSeq)),
		BranchID: branchID,
		RepoID:   env.repo.ID,
		Status:   git.WorktreeStatusPresent,
	}
	require.NoError(t, env.database.Create(wt).Error)
	require.NoError(t, env.swtStore.Add(&SessionWorktree{SessionID: sessionID, WorktreeID: wt.ID, Position: position}))
	return wt
}

func setupPRTestEnv(t *testing.T) *prTestEnv {
	t.Helper()

	database := testdb.New(t,
		&workspace.Workspace{},
		&git.Repo{},
		&git.Branch{},
		&git.Worktree{},
		&git.PullRequest{},
		&utmux.TmuxSession{},
		&Session{},
		&SessionWorktree{},
		&DismissedPR{},
		&claude.ClaudeSession{},
		&SessionAction{},
		&SessionSetupStep{},
	)

	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	dismissedPRStore := NewDismissedPRStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
	gitService := git.NewGitService(database)
	workspaceService := workspace.NewWorkspaceService(workspaceStore, gitService)

	repoPath := initTestRepo(t)
	branchName := "feature-pr"

	pushCmd := exec.Command("git", "-C", repoPath, "push", "origin", "main:"+branchName)
	pushCmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := pushCmd.CombinedOutput()
	require.NoError(t, err, "push remote branch failed: %s", string(out))

	repo := &git.Repo{Path: repoPath, FullName: "eleonorayaya/utena"}
	require.NoError(t, database.Create(repo).Error)

	repoID := repo.ID
	ws := &workspace.Workspace{Name: "utena", Path: repoPath, IsGitRepo: true, RepoID: &repoID}
	require.NoError(t, workspaceStore.Add(ws))

	branch := &git.Branch{Name: branchName, RepoID: repo.ID, ExistsLocal: false, ExistsRemote: true}
	require.NoError(t, database.Create(branch).Error)

	mock := utmux.NewMockRunner()
	tmuxService := createTmuxService(t, database, mock, bus)
	sessionActionStore := NewSessionActionStore(database)
	sessionWorktreeStore := NewSessionWorktreeStore(database)
	service := NewSessionService(sessionStore, sessionWorktreeStore, dismissedPRStore, sessionActionStore, NewSessionSetupStepStore(database), workspaceService, gitService, tmuxService, bus, "eqt/", t.TempDir(), t.TempDir())

	return &prTestEnv{
		service:          service,
		sessionStore:     sessionStore,
		swtStore:         sessionWorktreeStore,
		dismissedPRStore: dismissedPRStore,
		database:         database,
		workspace:        ws,
		repo:             repo,
		branch:           branch,
	}
}

func TestHandlePRUpdated_NewAssignedPR_CreatesSession(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	event := eventbus.Event{
		Type: git.EventPRUpdated,
		Data: git.PRUpdatedEvent{
			PullRequest: &git.PullRequest{
				RepoID:         env.repo.ID,
				Number:         42,
				HeadBranchID:   &branchID,
				Title:          "Test PR",
				State:          git.PRStateOpen,
				IsAssignedToMe: true,
			},
			Previous: nil,
			Repo:     env.repo,
		},
	}

	err := env.service.handlePRUpdated(ctx, event)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		sess, lookupErr := env.sessionStore.GetByBranchID(branchID)
		if lookupErr != nil {
			return false
		}
		return sess.Status == StatusActive || sess.Status == StatusBroken
	}, 5*time.Second, 50*time.Millisecond)

	sess, err := env.sessionStore.GetByBranchID(branchID)
	require.NoError(t, err)
	require.Equal(t, "feature-pr", sess.Name)
}

func TestHandlePRUpdated_SkipsUnassigned(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	event := eventbus.Event{
		Type: git.EventPRUpdated,
		Data: git.PRUpdatedEvent{
			PullRequest: &git.PullRequest{
				RepoID:         env.repo.ID,
				Number:         42,
				HeadBranchID:   &branchID,
				Title:          "Test PR",
				State:          git.PRStateOpen,
				IsAssignedToMe: false,
			},
			Previous: nil,
			Repo:     env.repo,
		},
	}

	err := env.service.handlePRUpdated(ctx, event)
	require.NoError(t, err)

	_, err = env.sessionStore.GetByBranchID(branchID)
	require.Error(t, err)
}

func TestHandlePRUpdated_SkipsNonOpenStates(t *testing.T) {
	cases := []struct {
		name  string
		state git.PRState
	}{
		{"draft", git.PRStateDraft},
		{"closed", git.PRStateClosed},
		{"merged", git.PRStateMerged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := setupPRTestEnv(t)
			ctx := context.Background()
			branchID := env.branch.ID
			event := eventbus.Event{
				Type: git.EventPRUpdated,
				Data: git.PRUpdatedEvent{
					PullRequest: &git.PullRequest{
						RepoID:         env.repo.ID,
						Number:         42,
						HeadBranchID:   &branchID,
						State:          tc.state,
						IsAssignedToMe: true,
					},
					Previous: nil,
					Repo:     env.repo,
				},
			}

			err := env.service.handlePRUpdated(ctx, event)
			require.NoError(t, err)

			_, err = env.sessionStore.GetByBranchID(branchID)
			require.Error(t, err)
		})
	}
}

func TestHandlePRUpdated_SkipsExistingSession(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	existing := &Session{
		Name:       "feature-pr",
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}
	require.NoError(t, env.sessionStore.Add(existing))
	env.attachBranchWorktree(t, existing.ID, branchID, 0)

	event := eventbus.Event{
		Type: git.EventPRUpdated,
		Data: git.PRUpdatedEvent{
			PullRequest: &git.PullRequest{
				RepoID:         env.repo.ID,
				Number:         42,
				HeadBranchID:   &branchID,
				Title:          "Test PR",
				State:          git.PRStateOpen,
				IsAssignedToMe: true,
			},
			Previous: nil,
			Repo:     env.repo,
		},
	}

	err := env.service.handlePRUpdated(ctx, event)
	require.NoError(t, err)

	sessions, err := env.sessionStore.List()
	require.NoError(t, err)
	require.Len(t, sessions, 1)
}

func TestHandlePRUpdated_CompletesSessionOnMerge(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	sess := &Session{
		Name:       "feature-pr",
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}
	require.NoError(t, env.sessionStore.Add(sess))
	env.attachBranchWorktree(t, sess.ID, branchID, 0)

	event := eventbus.Event{
		Type: git.EventPRUpdated,
		Data: git.PRUpdatedEvent{
			PullRequest: &git.PullRequest{
				Number:       42,
				HeadBranchID: &branchID,
				State:        git.PRStateMerged,
			},
			Previous: &git.PullRequest{State: git.PRStateOpen},
			Repo:     env.repo,
		},
	}

	err := env.service.handlePRUpdated(ctx, event)
	require.NoError(t, err)

	updated, err := env.sessionStore.GetByBranchID(branchID)
	require.NoError(t, err)
	require.Equal(t, StatusCompleted, updated.Status)
}

func TestHandlePRUpdated_IgnoresNonMerge(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	sess := &Session{
		Name:       "feature-pr",
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}
	require.NoError(t, env.sessionStore.Add(sess))
	env.attachBranchWorktree(t, sess.ID, branchID, 0)

	event := eventbus.Event{
		Type: git.EventPRUpdated,
		Data: git.PRUpdatedEvent{
			PullRequest: &git.PullRequest{
				Number:       42,
				HeadBranchID: &branchID,
				State:        git.PRStateClosed,
			},
			Previous: &git.PullRequest{State: git.PRStateOpen},
			Repo:     env.repo,
		},
	}

	err := env.service.handlePRUpdated(ctx, event)
	require.NoError(t, err)

	updated, err := env.sessionStore.GetByBranchID(branchID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, updated.Status)
}

func TestHandlePRUpdated_NewlyAssignedExistingPR_CreatesSession(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	event := eventbus.Event{
		Type: git.EventPRUpdated,
		Data: git.PRUpdatedEvent{
			PullRequest: &git.PullRequest{
				RepoID:         env.repo.ID,
				Number:         42,
				HeadBranchID:   &branchID,
				Title:          "Test PR",
				State:          git.PRStateOpen,
				IsAssignedToMe: true,
			},
			Previous: &git.PullRequest{State: git.PRStateOpen, IsAssignedToMe: false},
			Repo:     env.repo,
		},
	}

	err := env.service.handlePRUpdated(ctx, event)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		sess, lookupErr := env.sessionStore.GetByBranchID(branchID)
		if lookupErr != nil {
			return false
		}
		return sess.Status == StatusActive || sess.Status == StatusBroken
	}, 5*time.Second, 50*time.Millisecond)
}

func TestCompletedCleanupTask_ArchivesStale(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	sess := &Session{
		Name:       "stale-session",
		Status:     StatusCompleted,
		LastUsedAt: time.Now().Add(-10 * time.Minute),
	}
	require.NoError(t, env.sessionStore.Add(sess))
	env.attachBranchWorktree(t, sess.ID, branchID, 0)

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
		Name:       "attached-session",
		Status:     StatusCompleted,
		IsAttached: true,
		LastUsedAt: time.Now().Add(-10 * time.Minute),
	}
	require.NoError(t, env.sessionStore.Add(sess))
	env.attachBranchWorktree(t, sess.ID, branchID, 0)

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
		Name:       "recent-session",
		Status:     StatusCompleted,
		LastUsedAt: time.Now().Add(-1 * time.Minute),
	}
	require.NoError(t, env.sessionStore.Add(sess))
	env.attachBranchWorktree(t, sess.ID, branchID, 0)

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
		Type: git.EventPRUpdated,
		Data: git.PRUpdatedEvent{
			PullRequest: pr,
			Previous:    nil,
			Repo:        env.repo,
		},
	}

	err := env.service.handlePRUpdated(ctx, event)
	require.NoError(t, err)

	_, err = env.sessionStore.GetByBranchID(env.branch.ID)
	require.Error(t, err)
}

func TestHandlePRUpdated_BareWorkspace_CreatesWorktree(t *testing.T) {
	repoPath := initTestRepo(t)
	branchName := "feature/pr-branch"

	gitEnv := append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)

	worktreeStaging := filepath.Join(repoPath, ".worktrees", "feature-pr-branch")
	cmd := exec.Command("git", "-C", repoPath, "worktree", "add", "-b", branchName, worktreeStaging, "main")
	cmd.Env = gitEnv
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "pre-create branch failed: %s", string(out))

	pushCmd := exec.Command("git", "-C", worktreeStaging, "push", "-u", "origin", branchName)
	pushCmd.Env = gitEnv
	out, err = pushCmd.CombinedOutput()
	require.NoError(t, err, "push branch failed: %s", string(out))

	rmWtCmd := exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", worktreeStaging)
	rmWtCmd.Env = gitEnv
	out, err = rmWtCmd.CombinedOutput()
	require.NoError(t, err, "remove staging worktree failed: %s", string(out))

	database := setupTestDB(t)
	ctx := context.Background()
	gitService := git.NewGitService(database)
	require.NoError(t, gitService.MigrateToBare(ctx, repoPath, nil))

	repo := &git.Repo{Path: repoPath, FullName: "test/repo"}
	require.NoError(t, database.Create(repo).Error)

	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
	repoID := repo.ID
	wsGit := &workspace.Workspace{Name: "git-repo", Path: repoPath, IsGitRepo: true, IsBare: true, RepoID: &repoID}
	require.NoError(t, workspaceStore.Add(wsGit))

	branch := &git.Branch{Name: branchName, RepoID: repo.ID, ExistsRemote: true}
	require.NoError(t, database.Create(branch).Error)

	mock := utmux.NewMockRunner()
	tmuxService := createTmuxService(t, database, mock, bus)
	workspaceService := workspace.NewWorkspaceService(workspaceStore, gitService)
	dismissedPRStore := NewDismissedPRStore(database)
	sessionActionStore := NewSessionActionStore(database)
	sessionWorktreeStore := NewSessionWorktreeStore(database)
	service := NewSessionService(sessionStore, sessionWorktreeStore, dismissedPRStore, sessionActionStore, NewSessionSetupStepStore(database), workspaceService, gitService, tmuxService, bus, "eqt/", t.TempDir(), t.TempDir())

	branchID := branch.ID
	event := eventbus.Event{
		Type: git.EventPRUpdated,
		Data: git.PRUpdatedEvent{
			PullRequest: &git.PullRequest{
				RepoID:         repo.ID,
				Number:         42,
				HeadBranchID:   &branchID,
				Title:          "Test PR",
				State:          git.PRStateOpen,
				IsAssignedToMe: true,
			},
			Previous: nil,
			Repo:     repo,
		},
	}

	require.NoError(t, service.handlePRUpdated(ctx, event))

	require.Eventually(t, func() bool {
		_, lookupErr := sessionStore.GetByBranchID(branchID)
		return lookupErr == nil
	}, 10*time.Second, 50*time.Millisecond)

	sess, err := sessionStore.GetByBranchID(branchID)
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 10*time.Second)

	expectedWorktree := filepath.Join(service.sessionsRoot, sess.Name, "git-repo")
	info, err := os.Stat(expectedWorktree)
	require.NoError(t, err, "worktree should be created at %s", expectedWorktree)
	require.True(t, info.IsDir())

	tmuxName := SanitizeTmuxName(sess.Name)
	require.True(t, mock.HasSessionByName(tmuxName), "tmux session %s should exist", tmuxName)
	ts, err := tmuxService.GetSessionByName(tmuxName)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(service.sessionsRoot, sess.Name), ts.StartDir, "tmux session should start at the session root")
}

func TestHandlePRUpdated_NewAssignedPR_CreatesSessionAction(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	prURL := "https://github.com/eleonorayaya/utena/pull/42"
	event := eventbus.Event{
		Type: git.EventPRUpdated,
		Data: git.PRUpdatedEvent{
			PullRequest: &git.PullRequest{
				RepoID:         env.repo.ID,
				Number:         42,
				HeadBranchID:   &branchID,
				Title:          "Test PR",
				State:          git.PRStateOpen,
				IsAssignedToMe: true,
				HTMLURL:        prURL,
			},
			Previous: nil,
			Repo:     env.repo,
		},
	}

	err := env.service.handlePRUpdated(ctx, event)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		_, lookupErr := env.sessionStore.GetByBranchID(branchID)
		return lookupErr == nil
	}, 5*time.Second, 50*time.Millisecond)

	sess, err := env.sessionStore.GetByBranchID(branchID)
	require.NoError(t, err)

	actionStore := NewSessionActionStore(env.database)
	actions, err := actionStore.ListBySessionIDAndTrigger(sess.ID, TriggerOnCreate)
	require.NoError(t, err)
	require.Len(t, actions, 1)
	require.Equal(t, SessionActionTypeClaude, actions[0].Type)
	require.Equal(t, TriggerOnCreate, actions[0].Trigger)

	var opts ClaudeActionOptions
	require.NoError(t, json.Unmarshal([]byte(actions[0].Options), &opts))
	require.Contains(t, opts.Prompt, prURL)
}
