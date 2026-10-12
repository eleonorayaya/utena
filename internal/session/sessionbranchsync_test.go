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
	"github.com/stretchr/testify/require"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, string(out))
}

type branchSyncEnv struct {
	service  *SessionService
	store    *SessionStore
	sess     *Session
	repoPath string
	wtPath   string
}

func setupBranchSyncEnv(t *testing.T) *branchSyncEnv {
	t.Helper()
	repoPath := initTestRepo(t)
	gitIn(t, repoPath, "remote", "set-head", "origin", "main")
	service, store, _, wsID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	sess := &Session{Name: "multi"}
	require.NoError(t, createSessionLegacy(context.Background(), service, sess, wsID, "", "main"))
	waitForStatus(t, store, sess.ID, StatusActive, 60*time.Second)

	loaded, err := store.GetByID(sess.ID)
	require.NoError(t, err)
	return &branchSyncEnv{
		service:  service,
		store:    store,
		sess:     loaded,
		repoPath: repoPath,
		wtPath:   loaded.Worktrees[0].Worktree.Path,
	}
}

func (env *branchSyncEnv) reload(t *testing.T) *Session {
	t.Helper()
	sess, err := env.store.GetByID(env.sess.ID)
	require.NoError(t, err)
	return sess
}

func branchNames(sess *Session) []string {
	var names []string
	for _, sb := range sess.Branches {
		if sb.Branch != nil {
			names = append(names, sb.Branch.Name)
		}
	}
	return names
}

func TestRecordWorktreeBranch_RecordsOldAndNewAndRepoints(t *testing.T) {
	env := setupBranchSyncEnv(t)
	ctx := context.Background()
	gitIn(t, env.wtPath, "checkout", "-b", "eqt/second")

	require.NoError(t, env.service.RecordWorktreeBranch(ctx, env.wtPath, "eqt/second"))

	sess := env.reload(t)
	require.ElementsMatch(t, []string{"eqt/multi", "eqt/second"}, branchNames(sess))
	require.Equal(t, "eqt/second", sess.Worktrees[0].Worktree.Branch.Name)
}

func TestRecordWorktreeBranch_MatchesCanonicalPath(t *testing.T) {
	env := setupBranchSyncEnv(t)
	gitIn(t, env.wtPath, "checkout", "-b", "eqt/canonical")
	canonical, err := filepath.EvalSymlinks(env.wtPath)
	require.NoError(t, err)

	require.NoError(t, env.service.RecordWorktreeBranch(context.Background(), canonical, "eqt/canonical"))

	require.Equal(t, "eqt/canonical", env.reload(t).Worktrees[0].Worktree.Branch.Name)
}

func TestRecordWorktreeBranch_SkipsDetachedAndDefault(t *testing.T) {
	env := setupBranchSyncEnv(t)
	ctx := context.Background()

	require.NoError(t, env.service.RecordWorktreeBranch(ctx, env.wtPath, ""))
	require.NoError(t, env.service.RecordWorktreeBranch(ctx, env.wtPath, "main"))

	sess := env.reload(t)
	require.Empty(t, sess.Branches)
	require.Equal(t, "eqt/multi", sess.Worktrees[0].Worktree.Branch.Name)
}

func TestRecordWorktreeBranch_IgnoresUnknownPath(t *testing.T) {
	env := setupBranchSyncEnv(t)
	require.NoError(t, env.service.RecordWorktreeBranch(context.Background(), t.TempDir(), "eqt/elsewhere"))
	require.Empty(t, env.reload(t).Branches)
}

func TestRecordWorktreeBranch_SwitchBackIsIdempotent(t *testing.T) {
	env := setupBranchSyncEnv(t)
	ctx := context.Background()
	gitIn(t, env.wtPath, "checkout", "-b", "eqt/b")
	require.NoError(t, env.service.RecordWorktreeBranch(ctx, env.wtPath, "eqt/b"))
	gitIn(t, env.wtPath, "checkout", "eqt/multi")
	require.NoError(t, env.service.RecordWorktreeBranch(ctx, env.wtPath, "eqt/multi"))

	sess := env.reload(t)
	require.ElementsMatch(t, []string{"eqt/multi", "eqt/b"}, branchNames(sess))
	require.Equal(t, "eqt/multi", sess.Worktrees[0].Worktree.Branch.Name)
}

func TestRecordWorktreeBranch_ReplaysOpenPRsForNewBranch(t *testing.T) {
	env := setupBranchSyncEnv(t)
	ctx := context.Background()
	var got []string
	env.service.eventBus.Subscribe(eventbus.SessionNotification, func(_ context.Context, event eventbus.Event) error {
		if data, ok := event.Data.(eventbus.SessionNotificationEvent); ok {
			if n, ok := data.Data.(git.PRNotification); ok {
				got = append(got, n.Branch)
			}
		}
		return nil
	})

	branch, err := env.service.gitService.FindOrCreateBranch(ctx, "eqt/with-pr", env.sess.Worktrees[0].Worktree.RepoID)
	require.NoError(t, err)
	branchID := branch.ID
	require.NoError(t, env.store.db.Create(&git.PullRequest{RepoID: branch.RepoID, Number: 3, HeadBranchID: &branchID, State: git.PRStateOpen}).Error)
	require.NoError(t, env.store.db.Create(&git.PullRequest{RepoID: branch.RepoID, Number: 4, HeadBranchID: &branchID, State: git.PRStateMerged}).Error)

	gitIn(t, env.wtPath, "checkout", "-b", "eqt/with-pr")
	require.NoError(t, env.service.RecordWorktreeBranch(ctx, env.wtPath, "eqt/with-pr"))

	require.Equal(t, []string{"eqt/with-pr"}, got)
}

func TestSyncSessionBranches_PicksUpBranchChangedOnDisk(t *testing.T) {
	env := setupBranchSyncEnv(t)
	gitIn(t, env.wtPath, "checkout", "-b", "eqt/from-disk")

	env.service.SyncSessionBranches(context.Background(), env.sess)

	require.Equal(t, "eqt/from-disk", env.reload(t).Worktrees[0].Worktree.Branch.Name)
}

func localBranchExists(t *testing.T, repo string, name string) bool {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+name)
	return cmd.Run() == nil
}

func TestDeleteSession_DeletesPastBranchesOnlyWhenSafe(t *testing.T) {
	env := setupBranchSyncEnv(t)
	ctx := context.Background()
	record := func(name string) {
		require.NoError(t, env.service.RecordWorktreeBranch(ctx, env.wtPath, name))
	}

	gitIn(t, env.wtPath, "checkout", "-b", "eqt/pushed-past")
	record("eqt/pushed-past")

	gitIn(t, env.wtPath, "checkout", "-b", "eqt/merged-past")
	gitIn(t, env.wtPath, "commit", "--allow-empty", "-m", "merged work")
	record("eqt/merged-past")

	gitIn(t, env.wtPath, "checkout", "-b", "eqt/unpushed-past", "main")
	gitIn(t, env.wtPath, "commit", "--allow-empty", "-m", "local only")
	record("eqt/unpushed-past")

	gitIn(t, env.wtPath, "checkout", "-b", "eqt/current", "main")
	record("eqt/current")

	sess := env.reload(t)
	for _, b := range sess.PastBranches() {
		if b.Name == "eqt/merged-past" {
			id := b.ID
			require.NoError(t, env.store.db.Create(&git.PullRequest{RepoID: b.RepoID, Number: 11, HeadBranchID: &id, State: git.PRStateMerged}).Error)
		}
	}

	_, err := env.service.ArchiveSession(ctx, sess.ID)
	require.NoError(t, err)
	require.NoError(t, env.service.DeleteSession(ctx, sess.ID, true, false))

	require.False(t, localBranchExists(t, env.repoPath, "eqt/current"))
	require.False(t, localBranchExists(t, env.repoPath, "eqt/multi"))
	require.False(t, localBranchExists(t, env.repoPath, "eqt/pushed-past"))
	require.False(t, localBranchExists(t, env.repoPath, "eqt/merged-past"))
	require.True(t, localBranchExists(t, env.repoPath, "eqt/unpushed-past"), "unmerged local work must survive")
}
