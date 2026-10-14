package session

import (
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db"
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

func TestSession_BranchName_ReadsCurrentAndHistory(t *testing.T) {
	sess := Session{
		Worktrees: []SessionWorktree{{Worktree: &git.Worktree{BranchID: 2, Branch: &git.Branch{Name: "current"}}}},
		Branches:  []SessionBranch{{BranchID: 1, Branch: &git.Branch{Name: "past"}}},
	}
	require.Equal(t, "current", sess.branchName(2))
	require.Equal(t, "past", sess.branchName(1))
	require.Equal(t, "", sess.branchName(9))
}

func TestSessionBranchStore_AddIsIdempotent(t *testing.T) {
	database := setupTestDB(t)
	store := NewSessionBranchStore(database)
	sess := &Session{Name: "s", Status: StatusActive}
	require.NoError(t, database.Create(sess).Error)
	branch := &git.Branch{Name: "b", RepoID: 1}
	require.NoError(t, database.Create(branch).Error)

	first := time.Now().Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, store.Add(&SessionBranch{SessionID: sess.ID, BranchID: branch.ID, FirstSeenAt: first}))
	require.NoError(t, store.Add(&SessionBranch{SessionID: sess.ID, BranchID: branch.ID, FirstSeenAt: time.Now()}))

	var rows []SessionBranch
	require.NoError(t, database.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.True(t, rows[0].FirstSeenAt.Equal(first), "first-seen time must be preserved")
}

func TestSessionStore_LoadsBranchesAndFindsSessionByHistoryBranch(t *testing.T) {
	database, wsID, branch, _ := setupTestDBWithGitAndMux(t)
	store := NewSessionStore(database)
	swtStore := NewSessionWorktreeStore(database)

	sess := &Session{Name: "multi", Status: StatusActive, LastUsedAt: time.Now()}
	addTestSession(t, store, swtStore, sess, wsID)

	past := &git.Branch{Name: "past-branch", RepoID: branch.RepoID}
	require.NoError(t, database.Create(past).Error)
	require.NoError(t, NewSessionBranchStore(database).Add(&SessionBranch{SessionID: sess.ID, BranchID: past.ID, FirstSeenAt: time.Now()}))

	loaded, err := store.GetByID(sess.ID)
	require.NoError(t, err)
	require.Len(t, loaded.Branches, 1)
	require.NotNil(t, loaded.Branches[0].Branch)
	require.Equal(t, "past-branch", loaded.Branches[0].Branch.Name)

	found, err := store.GetByBranchID(past.ID)
	require.NoError(t, err)
	require.Equal(t, sess.ID, found.ID)
}

func newSharedBranchFixture(t *testing.T) (*SessionStore, *SessionWorktreeStore, *SessionBranchStore, uint, *git.Branch, func(sess *Session)) {
	t.Helper()
	database, wsID, branch, _ := setupTestDBWithGitAndMux(t)
	store := NewSessionStore(database)
	swtStore := NewSessionWorktreeStore(database)
	sbStore := NewSessionBranchStore(database)
	shared := &git.Branch{Name: "shared", RepoID: branch.RepoID}
	require.NoError(t, database.Create(shared).Error)
	add := func(sess *Session) {
		require.NoError(t, store.Add(sess))
	}
	return store, swtStore, sbStore, wsID, shared, add
}

func TestSessionStore_GetByBranchID_CurrentWorktreeBeatsHistory(t *testing.T) {
	store, swtStore, sbStore, wsID, shared, add := newSharedBranchFixture(t)

	old := &Session{Name: "old", Status: StatusArchived, LastUsedAt: time.Now().Add(-time.Hour)}
	add(old)
	attachTestWorktree(t, store.db, swtStore, old.ID, wsID, 0)
	require.NoError(t, sbStore.Add(&SessionBranch{SessionID: old.ID, BranchID: shared.ID, FirstSeenAt: time.Now()}))

	current := &Session{Name: "current", Status: StatusActive, LastUsedAt: time.Now()}
	add(current)
	curWt := attachTestWorktree(t, store.db, swtStore, current.ID, wsID, 0)
	require.NoError(t, store.db.Model(curWt).Update("branch_id", shared.ID).Error)

	found, err := store.GetByBranchID(shared.ID)
	require.NoError(t, err)
	require.Equal(t, current.ID, found.ID)
}

func TestSessionStore_GetByBranchID_PrefersLiveSessionAmongHistoryMatches(t *testing.T) {
	store, swtStore, sbStore, wsID, shared, add := newSharedBranchFixture(t)

	old := &Session{Name: "old", Status: StatusArchived, LastUsedAt: time.Now().Add(-time.Hour)}
	add(old)
	attachTestWorktree(t, store.db, swtStore, old.ID, wsID, 0)
	require.NoError(t, sbStore.Add(&SessionBranch{SessionID: old.ID, BranchID: shared.ID, FirstSeenAt: time.Now()}))

	live := &Session{Name: "live", Status: StatusActive, LastUsedAt: time.Now().Add(-2 * time.Hour)}
	add(live)
	attachTestWorktree(t, store.db, swtStore, live.ID, wsID, 0)
	require.NoError(t, sbStore.Add(&SessionBranch{SessionID: live.ID, BranchID: shared.ID, FirstSeenAt: time.Now()}))

	found, err := store.GetByBranchID(shared.ID)
	require.NoError(t, err)
	require.Equal(t, live.ID, found.ID)
}

func TestSessionStore_GetByBranchID_MostRecentlyUsedAmongEquals(t *testing.T) {
	store, swtStore, sbStore, wsID, shared, add := newSharedBranchFixture(t)

	first := &Session{Name: "first", Status: StatusActive, LastUsedAt: time.Now().Add(-time.Hour)}
	add(first)
	attachTestWorktree(t, store.db, swtStore, first.ID, wsID, 0)
	require.NoError(t, sbStore.Add(&SessionBranch{SessionID: first.ID, BranchID: shared.ID, FirstSeenAt: time.Now()}))

	second := &Session{Name: "second", Status: StatusActive, LastUsedAt: time.Now()}
	add(second)
	attachTestWorktree(t, store.db, swtStore, second.ID, wsID, 0)
	require.NoError(t, sbStore.Add(&SessionBranch{SessionID: second.ID, BranchID: shared.ID, FirstSeenAt: time.Now()}))

	found, err := store.GetByBranchID(shared.ID)
	require.NoError(t, err)
	require.Equal(t, second.ID, found.ID)
}

func TestSessionBranch_MigratesTableCreatedWithoutWorktreeID(t *testing.T) {
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	require.NoError(t, database.Exec("CREATE TABLE `session_branches` (`id` integer PRIMARY KEY AUTOINCREMENT,`created_at` datetime,`updated_at` datetime,`deleted_at` datetime,`session_id` integer NOT NULL,`branch_id` integer NOT NULL,`first_seen_at` datetime)").Error)
	require.NoError(t, database.Exec("INSERT INTO `session_branches` (`session_id`,`branch_id`) VALUES (1, 1)").Error)

	require.NoError(t, database.Migrate(&SessionBranch{}))
}
