package git

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/require"
)

func setupPRTestDB(t *testing.T) db.Database {
	return testdb.New(t, &Repo{}, &Branch{}, &PullRequest{})
}

func createPRTestRepo(t *testing.T, store *RepoStore) *Repo {
	t.Helper()
	repo := &Repo{Path: "/test/repo", FullName: "owner/repo"}
	require.NoError(t, store.Add(repo))
	return repo
}

func TestPRStore_AddAndGetByID(t *testing.T) {
	database := setupPRTestDB(t)
	repoStore := NewRepoStore(database)
	prStore := NewPRStore(database)

	repo := createPRTestRepo(t, repoStore)
	pr := &PullRequest{
		RepoID:      repo.ID,
		Number:      42,
		Title:       "Add feature",
		State:       PRStateOpen,
		HTMLURL:     "https://github.com/owner/repo/pull/42",
		AuthorLogin: "octocat",
	}
	require.NoError(t, prStore.Add(pr))
	require.NotZero(t, pr.ID)

	found, err := prStore.GetByID(pr.ID)
	require.NoError(t, err)
	require.Equal(t, 42, found.Number)
	require.Equal(t, "Add feature", found.Title)
	require.Equal(t, PRStateOpen, found.State)
	require.Equal(t, repo.ID, found.RepoID)
	require.Equal(t, "octocat", found.AuthorLogin)
}

func TestPRStore_GetByRepoAndNumber(t *testing.T) {
	database := setupPRTestDB(t)
	repoStore := NewRepoStore(database)
	prStore := NewPRStore(database)

	repo := createPRTestRepo(t, repoStore)
	pr := &PullRequest{
		RepoID: repo.ID,
		Number: 7,
		Title:  "Fix bug",
		State:  PRStateOpen,
	}
	require.NoError(t, prStore.Add(pr))

	found, err := prStore.GetByRepoAndNumber(repo.ID, 7)
	require.NoError(t, err)
	require.Equal(t, pr.ID, found.ID)
	require.Equal(t, "Fix bug", found.Title)
}

func TestPRStore_UniqueConstraint(t *testing.T) {
	database := setupPRTestDB(t)
	repoStore := NewRepoStore(database)
	prStore := NewPRStore(database)

	repo := createPRTestRepo(t, repoStore)
	require.NoError(t, prStore.Add(&PullRequest{RepoID: repo.ID, Number: 1, Title: "First", State: PRStateOpen}))

	err := prStore.Add(&PullRequest{RepoID: repo.ID, Number: 1, Title: "Duplicate", State: PRStateOpen})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPRAlreadyExists)

	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, common.CategoryConflict, appErr.Category)
	require.Contains(t, err.Error(), "pull request")
	require.Contains(t, err.Error(), "#1")
}

func TestPRStore_ListByBranch(t *testing.T) {
	database := setupPRTestDB(t)
	repoStore := NewRepoStore(database)
	branchStore := NewBranchStore(database)
	prStore := NewPRStore(database)

	repo := createPRTestRepo(t, repoStore)
	branch := &Branch{Name: "feature", RepoID: repo.ID, ExistsLocal: true}
	require.NoError(t, branchStore.Add(branch))

	otherBranch := &Branch{Name: "other", RepoID: repo.ID, ExistsLocal: true}
	require.NoError(t, branchStore.Add(otherBranch))

	require.NoError(t, prStore.Add(&PullRequest{RepoID: repo.ID, Number: 1, HeadBranchID: &branch.ID, Title: "PR 1", State: PRStateOpen}))
	require.NoError(t, prStore.Add(&PullRequest{RepoID: repo.ID, Number: 2, HeadBranchID: &branch.ID, Title: "PR 2", State: PRStateOpen}))
	require.NoError(t, prStore.Add(&PullRequest{RepoID: repo.ID, Number: 3, HeadBranchID: &otherBranch.ID, Title: "PR 3", State: PRStateOpen}))

	prs := prStore.ListByBranch(branch.ID)
	require.Len(t, prs, 2)

	otherPRs := prStore.ListByBranch(otherBranch.ID)
	require.Len(t, otherPRs, 1)
}

func TestPRStore_ListByState(t *testing.T) {
	database := setupPRTestDB(t)
	repoStore := NewRepoStore(database)
	prStore := NewPRStore(database)

	repo := createPRTestRepo(t, repoStore)
	require.NoError(t, prStore.Add(&PullRequest{RepoID: repo.ID, Number: 1, Title: "Open PR", State: PRStateOpen}))
	require.NoError(t, prStore.Add(&PullRequest{RepoID: repo.ID, Number: 2, Title: "Merged PR", State: PRStateMerged}))
	require.NoError(t, prStore.Add(&PullRequest{RepoID: repo.ID, Number: 3, Title: "Closed PR", State: PRStateClosed}))
	require.NoError(t, prStore.Add(&PullRequest{RepoID: repo.ID, Number: 4, Title: "Another Open", State: PRStateOpen}))

	openPRs := prStore.ListByState(repo.ID, PRStateOpen)
	require.Len(t, openPRs, 2)

	mergedPRs := prStore.ListByState(repo.ID, PRStateMerged)
	require.Len(t, mergedPRs, 1)

	closedPRs := prStore.ListByState(repo.ID, PRStateClosed)
	require.Len(t, closedPRs, 1)
}

func TestPRStore_UpsertCreates(t *testing.T) {
	database := setupPRTestDB(t)
	repoStore := NewRepoStore(database)
	prStore := NewPRStore(database)

	repo := createPRTestRepo(t, repoStore)
	pr := &PullRequest{RepoID: repo.ID, Number: 10, Title: "New PR", State: PRStateOpen}
	require.NoError(t, prStore.Upsert(pr))
	require.NotZero(t, pr.ID)

	found, err := prStore.GetByRepoAndNumber(repo.ID, 10)
	require.NoError(t, err)
	require.Equal(t, "New PR", found.Title)
}

func TestPRStore_UpsertUpdates(t *testing.T) {
	database := setupPRTestDB(t)
	repoStore := NewRepoStore(database)
	prStore := NewPRStore(database)

	repo := createPRTestRepo(t, repoStore)
	pr := &PullRequest{RepoID: repo.ID, Number: 10, Title: "Original", State: PRStateOpen}
	require.NoError(t, prStore.Add(pr))
	originalID := pr.ID

	updated := &PullRequest{RepoID: repo.ID, Number: 10, Title: "Updated", State: PRStateMerged}
	require.NoError(t, prStore.Upsert(updated))

	found, err := prStore.GetByRepoAndNumber(repo.ID, 10)
	require.NoError(t, err)
	require.Equal(t, originalID, found.ID)
	require.Equal(t, "Updated", found.Title)
	require.Equal(t, PRStateMerged, found.State)
}
