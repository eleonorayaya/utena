package git

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/stretchr/testify/require"
)

func setupWorktreeTestDB(t *testing.T) db.Database {
	t.Helper()
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	require.NoError(t, database.Migrate(&Repo{}, &Branch{}, &Worktree{}))
	t.Cleanup(func() { database.Close() })
	return database
}

func createWorktreeTestRepoAndBranch(t *testing.T, database db.Database, path, branchName string) (*Repo, *Branch) {
	t.Helper()
	repoStore := NewRepoStore(database)
	branchStore := NewBranchStore(database)

	repo := &Repo{Path: path, FullName: "owner/repo"}
	require.NoError(t, repoStore.Add(repo))

	branch := &Branch{Name: branchName, RepoID: repo.ID, ExistsLocal: true}
	require.NoError(t, branchStore.Add(branch))

	return repo, branch
}

func TestWorktreeStore_AddAndGetByBranchID(t *testing.T) {
	database := setupWorktreeTestDB(t)
	repo, branch := createWorktreeTestRepoAndBranch(t, database, "/test/repo", "feature")
	store := NewWorktreeStore(database)

	wt := &Worktree{Path: "/test/repo/.worktrees/feature", BranchID: branch.ID, RepoID: repo.ID}
	require.NoError(t, store.Add(wt))
	require.NotZero(t, wt.ID)

	found, err := store.GetByBranchID(branch.ID)
	require.NoError(t, err)
	require.NotNil(t, found)
	require.Equal(t, wt.ID, found.ID)
	require.Equal(t, "/test/repo/.worktrees/feature", found.Path)
	require.Equal(t, branch.ID, found.BranchID)
	require.Equal(t, repo.ID, found.RepoID)
}

func TestWorktreeStore_PathUniqueness(t *testing.T) {
	database := setupWorktreeTestDB(t)
	repoStore := NewRepoStore(database)
	branchStore := NewBranchStore(database)
	store := NewWorktreeStore(database)

	repo := &Repo{Path: "/test/repo", FullName: "owner/repo"}
	require.NoError(t, repoStore.Add(repo))

	branch1 := &Branch{Name: "branch1", RepoID: repo.ID, ExistsLocal: true}
	require.NoError(t, branchStore.Add(branch1))
	branch2 := &Branch{Name: "branch2", RepoID: repo.ID, ExistsLocal: true}
	require.NoError(t, branchStore.Add(branch2))

	require.NoError(t, store.Add(&Worktree{Path: "/test/repo/.worktrees/same-path", BranchID: branch1.ID, RepoID: repo.ID}))

	err := store.Add(&Worktree{Path: "/test/repo/.worktrees/same-path", BranchID: branch2.ID, RepoID: repo.ID})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrWorktreeAlreadyExists)
}

func TestWorktreeStore_BranchIDUniqueness(t *testing.T) {
	database := setupWorktreeTestDB(t)
	repo, branch := createWorktreeTestRepoAndBranch(t, database, "/test/repo", "feature")
	store := NewWorktreeStore(database)

	require.NoError(t, store.Add(&Worktree{Path: "/test/repo/.worktrees/wt1", BranchID: branch.ID, RepoID: repo.ID}))

	err := store.Add(&Worktree{Path: "/test/repo/.worktrees/wt2", BranchID: branch.ID, RepoID: repo.ID})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrWorktreeAlreadyExists)
}

func TestWorktreeStore_DeleteByBranchID(t *testing.T) {
	database := setupWorktreeTestDB(t)
	repo, branch := createWorktreeTestRepoAndBranch(t, database, "/test/repo", "feature")
	store := NewWorktreeStore(database)

	require.NoError(t, store.Add(&Worktree{Path: "/test/repo/.worktrees/feature", BranchID: branch.ID, RepoID: repo.ID}))

	require.NoError(t, store.DeleteByBranchID(branch.ID))

	_, err := store.GetByBranchID(branch.ID)
	require.ErrorIs(t, err, ErrWorktreeNotFound)

	worktrees := store.ListByRepo(repo.ID)
	require.Empty(t, worktrees)
}

func TestWorktreeStore_GetByBranchID_ReturnsErrorWhenNotFound(t *testing.T) {
	database := setupWorktreeTestDB(t)
	store := NewWorktreeStore(database)

	_, err := store.GetByBranchID(999)
	require.ErrorIs(t, err, ErrWorktreeNotFound)
}

func TestWorktreeStore_ListByRepo(t *testing.T) {
	database := setupWorktreeTestDB(t)
	repoStore := NewRepoStore(database)
	branchStore := NewBranchStore(database)
	store := NewWorktreeStore(database)

	repo1 := &Repo{Path: "/test/repo1", FullName: "owner/repo1"}
	require.NoError(t, repoStore.Add(repo1))
	repo2 := &Repo{Path: "/test/repo2", FullName: "owner/repo2"}
	require.NoError(t, repoStore.Add(repo2))

	b1 := &Branch{Name: "feat1", RepoID: repo1.ID, ExistsLocal: true}
	require.NoError(t, branchStore.Add(b1))
	b2 := &Branch{Name: "feat2", RepoID: repo1.ID, ExistsLocal: true}
	require.NoError(t, branchStore.Add(b2))
	b3 := &Branch{Name: "feat1", RepoID: repo2.ID, ExistsLocal: true}
	require.NoError(t, branchStore.Add(b3))

	require.NoError(t, store.Add(&Worktree{Path: "/test/repo1/.worktrees/feat1", BranchID: b1.ID, RepoID: repo1.ID}))
	require.NoError(t, store.Add(&Worktree{Path: "/test/repo1/.worktrees/feat2", BranchID: b2.ID, RepoID: repo1.ID}))
	require.NoError(t, store.Add(&Worktree{Path: "/test/repo2/.worktrees/feat1", BranchID: b3.ID, RepoID: repo2.ID}))

	worktrees1 := store.ListByRepo(repo1.ID)
	require.Len(t, worktrees1, 2)

	worktrees2 := store.ListByRepo(repo2.ID)
	require.Len(t, worktrees2, 1)

	worktrees3 := store.ListByRepo(999)
	require.Empty(t, worktrees3)
}
