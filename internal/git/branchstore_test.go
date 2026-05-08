package git

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/require"
)

func setupBranchTestDB(t *testing.T) db.Database {
	return testdb.New(t, &Repo{}, &Branch{})
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

func TestBranch_DeriveStatus(t *testing.T) {
	cases := []struct {
		name   string
		branch Branch
		want   BranchStatus
	}{
		{
			name:   "fresh empty record stays pending",
			branch: Branch{},
			want:   BranchStatusPending,
		},
		{
			name:   "explicit pending stays pending when bools false",
			branch: Branch{Status: BranchStatusPending},
			want:   BranchStatusPending,
		},
		{
			name:   "exists local flips to tracked",
			branch: Branch{ExistsLocal: true},
			want:   BranchStatusTracked,
		},
		{
			name:   "exists remote flips to tracked",
			branch: Branch{ExistsRemote: true},
			want:   BranchStatusTracked,
		},
		{
			name:   "previously tracked with both bools false flips to gone",
			branch: Branch{Status: BranchStatusTracked},
			want:   BranchStatusGone,
		},
		{
			name:   "previously gone reappears as tracked when bool flips",
			branch: Branch{Status: BranchStatusGone, ExistsRemote: true},
			want:   BranchStatusTracked,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := tc.branch
			require.Equal(t, tc.want, b.DeriveStatus())
		})
	}
}

func TestBranchStore_BackfillStatus(t *testing.T) {
	database := setupBranchTestDB(t)
	repoStore := NewRepoStore(database)
	branchStore := NewBranchStore(database)

	repo := createTestRepo(t, repoStore)

	tracked := &Branch{Name: "tracked-local", RepoID: repo.ID, ExistsLocal: true}
	require.NoError(t, branchStore.Add(tracked))
	trackedRemote := &Branch{Name: "tracked-remote", RepoID: repo.ID, ExistsRemote: true}
	require.NoError(t, branchStore.Add(trackedRemote))
	noObs := &Branch{Name: "no-obs", RepoID: repo.ID}
	require.NoError(t, branchStore.Add(noObs))

	require.NoError(t, database.Exec("UPDATE branches SET status = '' WHERE 1=1").Error)

	require.NoError(t, branchStore.BackfillStatus())

	got1, err := branchStore.GetByID(tracked.ID)
	require.NoError(t, err)
	require.Equal(t, BranchStatusTracked, got1.Status)

	got2, err := branchStore.GetByID(trackedRemote.ID)
	require.NoError(t, err)
	require.Equal(t, BranchStatusTracked, got2.Status)

	got3, err := branchStore.GetByID(noObs.ID)
	require.NoError(t, err)
	require.Equal(t, BranchStatusPending, got3.Status)
}

func TestBranchStore_BackfillStatus_PreservesExplicit(t *testing.T) {
	database := setupBranchTestDB(t)
	repoStore := NewRepoStore(database)
	branchStore := NewBranchStore(database)

	repo := createTestRepo(t, repoStore)

	gone := &Branch{Name: "old", RepoID: repo.ID, Status: BranchStatusGone}
	require.NoError(t, branchStore.Add(gone))

	require.NoError(t, branchStore.BackfillStatus())

	got, err := branchStore.GetByID(gone.ID)
	require.NoError(t, err)
	require.Equal(t, BranchStatusGone, got.Status)
}
