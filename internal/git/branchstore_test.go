package git

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/stretchr/testify/require"
)

func setupBranchTestDB(t *testing.T) db.Database {
	t.Helper()
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	require.NoError(t, database.Migrate(&Repo{}, &Branch{}))
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Logf("close database: %v", err)
		}
	})
	return database
}

func createTestRepo(t *testing.T, store *RepoStore) *Repo {
	t.Helper()
	repo := &Repo{Path: "/test/repo", FullName: "owner/repo"}
	require.NoError(t, store.Add(repo))
	return repo
}

func TestBranchStore_AddAndGetByID(t *testing.T) {
	database := setupBranchTestDB(t)
	repoStore := NewRepoStore(database)
	branchStore := NewBranchStore(database)

	repo := createTestRepo(t, repoStore)
	branch := &Branch{
		Name:         "main",
		RepoID:       repo.ID,
		ExistsLocal:  true,
		ExistsRemote: true,
		IsDirty:      false,
	}
	require.NoError(t, branchStore.Add(branch))
	require.NotZero(t, branch.ID)

	found, err := branchStore.GetByID(branch.ID)
	require.NoError(t, err)
	require.Equal(t, "main", found.Name)
	require.Equal(t, repo.ID, found.RepoID)
	require.True(t, found.ExistsLocal)
	require.True(t, found.ExistsRemote)
	require.False(t, found.IsDirty)
}

func TestBranchStore_GetByNameAndRepo(t *testing.T) {
	database := setupBranchTestDB(t)
	repoStore := NewRepoStore(database)
	branchStore := NewBranchStore(database)

	repo := createTestRepo(t, repoStore)
	branch := &Branch{Name: "feature", RepoID: repo.ID, ExistsLocal: true}
	require.NoError(t, branchStore.Add(branch))

	found, err := branchStore.GetByNameAndRepo("feature", repo.ID)
	require.NoError(t, err)
	require.Equal(t, branch.ID, found.ID)
	require.Equal(t, "feature", found.Name)
}

func TestBranchStore_UniqueConstraint(t *testing.T) {
	database := setupBranchTestDB(t)
	repoStore := NewRepoStore(database)
	branchStore := NewBranchStore(database)

	repo := createTestRepo(t, repoStore)
	require.NoError(t, branchStore.Add(&Branch{Name: "main", RepoID: repo.ID}))

	err := branchStore.Add(&Branch{Name: "main", RepoID: repo.ID})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrBranchAlreadyExists)
}

func TestBranchStore_SameNameDifferentRepo(t *testing.T) {
	database := setupBranchTestDB(t)
	repoStore := NewRepoStore(database)
	branchStore := NewBranchStore(database)

	repo1 := createTestRepo(t, repoStore)
	repo2 := &Repo{Path: "/test/repo2", FullName: "owner/repo2"}
	require.NoError(t, repoStore.Add(repo2))

	require.NoError(t, branchStore.Add(&Branch{Name: "main", RepoID: repo1.ID}))
	require.NoError(t, branchStore.Add(&Branch{Name: "main", RepoID: repo2.ID}))

	branches1 := branchStore.ListByRepo(repo1.ID)
	branches2 := branchStore.ListByRepo(repo2.ID)
	require.Len(t, branches1, 1)
	require.Len(t, branches2, 1)
}

func TestBranchStore_BaseBranchSelfReference(t *testing.T) {
	database := setupBranchTestDB(t)
	repoStore := NewRepoStore(database)
	branchStore := NewBranchStore(database)

	repo := createTestRepo(t, repoStore)
	baseBranch := &Branch{Name: "main", RepoID: repo.ID, ExistsLocal: true}
	require.NoError(t, branchStore.Add(baseBranch))

	featureBranch := &Branch{
		Name:         "feature",
		RepoID:       repo.ID,
		BaseBranchID: &baseBranch.ID,
		ExistsLocal:  true,
	}
	require.NoError(t, branchStore.Add(featureBranch))

	found, err := branchStore.GetByID(featureBranch.ID)
	require.NoError(t, err)
	require.NotNil(t, found.BaseBranchID)
	require.Equal(t, baseBranch.ID, *found.BaseBranchID)
}

func TestBranchStore_ListByRepo(t *testing.T) {
	database := setupBranchTestDB(t)
	repoStore := NewRepoStore(database)
	branchStore := NewBranchStore(database)

	repo1 := createTestRepo(t, repoStore)
	repo2 := &Repo{Path: "/test/repo2", FullName: "owner/repo2"}
	require.NoError(t, repoStore.Add(repo2))

	require.NoError(t, branchStore.Add(&Branch{Name: "main", RepoID: repo1.ID}))
	require.NoError(t, branchStore.Add(&Branch{Name: "develop", RepoID: repo1.ID}))
	require.NoError(t, branchStore.Add(&Branch{Name: "main", RepoID: repo2.ID}))

	branches1 := branchStore.ListByRepo(repo1.ID)
	require.Len(t, branches1, 2)

	branches2 := branchStore.ListByRepo(repo2.ID)
	require.Len(t, branches2, 1)
}

func TestBranchStore_UpsertCreates(t *testing.T) {
	database := setupBranchTestDB(t)
	repoStore := NewRepoStore(database)
	branchStore := NewBranchStore(database)

	repo := createTestRepo(t, repoStore)
	branch := &Branch{Name: "new-branch", RepoID: repo.ID, ExistsLocal: true}
	require.NoError(t, branchStore.Upsert(branch))
	require.NotZero(t, branch.ID)

	found, err := branchStore.GetByNameAndRepo("new-branch", repo.ID)
	require.NoError(t, err)
	require.True(t, found.ExistsLocal)
}

func TestBranchStore_UpsertUpdates(t *testing.T) {
	database := setupBranchTestDB(t)
	repoStore := NewRepoStore(database)
	branchStore := NewBranchStore(database)

	repo := createTestRepo(t, repoStore)
	branch := &Branch{Name: "main", RepoID: repo.ID, ExistsLocal: true, ExistsRemote: false}
	require.NoError(t, branchStore.Add(branch))
	originalID := branch.ID

	updated := &Branch{Name: "main", RepoID: repo.ID, ExistsLocal: true, ExistsRemote: true}
	require.NoError(t, branchStore.Upsert(updated))

	found, err := branchStore.GetByNameAndRepo("main", repo.ID)
	require.NoError(t, err)
	require.Equal(t, originalID, found.ID)
	require.True(t, found.ExistsRemote)
}
