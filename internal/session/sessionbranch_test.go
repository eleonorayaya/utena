package session

import (
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/git"
	"github.com/stretchr/testify/require"
)

func TestSession_BranchIDs_UnionsCurrentAndHistory(t *testing.T) {
	sess := Session{
		Worktrees: []SessionWorktree{
			{Worktree: &git.Worktree{BranchID: 3}},
			{Worktree: &git.Worktree{BranchID: 0}},
			{Worktree: nil},
		},
		Branches: []SessionBranch{{BranchID: 1}, {BranchID: 3}, {BranchID: 2}},
	}
	require.Equal(t, []uint{3, 1, 2}, sess.BranchIDs())
}

func TestSession_PastBranches_ExcludesCurrent(t *testing.T) {
	sess := Session{
		Worktrees: []SessionWorktree{{Worktree: &git.Worktree{BranchID: 2}}},
		Branches: []SessionBranch{
			{BranchID: 1, Branch: &git.Branch{Name: "first"}},
			{BranchID: 2, Branch: &git.Branch{Name: "current"}},
			{BranchID: 4},
		},
	}
	past := sess.PastBranches()
	require.Len(t, past, 1)
	require.Equal(t, "first", past[0].Name)
}

func TestSession_FilterPRs(t *testing.T) {
	one, two, other := uint(1), uint(2), uint(9)
	sess := Session{
		Worktrees: []SessionWorktree{{Worktree: &git.Worktree{BranchID: 2}}},
		Branches:  []SessionBranch{{BranchID: 1}},
	}
	prs := []git.PullRequest{
		{Number: 10, HeadBranchID: &one},
		{Number: 20, HeadBranchID: &two},
		{Number: 30, HeadBranchID: &other},
		{Number: 40},
	}
	got := sess.FilterPRs(prs)
	require.Len(t, got, 2)
	require.Equal(t, 10, got[0].Number)
	require.Equal(t, 20, got[1].Number)
}

func TestSessionBranchStore_AddIsIdempotent(t *testing.T) {
	database := setupTestDB(t)
	store := NewSessionBranchStore(database)
	sess := &Session{Name: "s", Status: StatusActive}
	require.NoError(t, database.Create(sess).Error)
	branch := &git.Branch{Name: "b", RepoID: 1}
	require.NoError(t, database.Create(branch).Error)

	first := time.Now().Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, store.Add(&SessionBranch{SessionID: sess.ID, WorktreeID: 1, BranchID: branch.ID, FirstSeenAt: first}))
	require.NoError(t, store.Add(&SessionBranch{SessionID: sess.ID, WorktreeID: 1, BranchID: branch.ID, FirstSeenAt: time.Now()}))

	var rows []SessionBranch
	require.NoError(t, database.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.True(t, rows[0].FirstSeenAt.Equal(first), "first-seen time must be preserved")
}

func TestSessionStore_LoadsBranchesAndFindsSessionByHistoryBranch(t *testing.T) {
	database, wsID, branch, _ := setupTestDBWithGitAndTmux(t)
	store := NewSessionStore(database)
	swtStore := NewSessionWorktreeStore(database)

	sess := &Session{Name: "multi", Status: StatusActive, LastUsedAt: time.Now()}
	addTestSession(t, store, swtStore, sess, wsID)

	past := &git.Branch{Name: "past-branch", RepoID: branch.RepoID}
	require.NoError(t, database.Create(past).Error)
	require.NoError(t, NewSessionBranchStore(database).Add(&SessionBranch{SessionID: sess.ID, WorktreeID: 1, BranchID: past.ID, FirstSeenAt: time.Now()}))

	loaded, err := store.GetByID(sess.ID)
	require.NoError(t, err)
	require.Len(t, loaded.Branches, 1)
	require.NotNil(t, loaded.Branches[0].Branch)
	require.Equal(t, "past-branch", loaded.Branches[0].Branch.Name)

	found, err := store.GetByBranchID(past.ID)
	require.NoError(t, err)
	require.Equal(t, sess.ID, found.ID)
}
