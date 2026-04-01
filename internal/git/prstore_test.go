package git

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/db"
	"github.com/stretchr/testify/require"
)

func setupPRTestDB(t *testing.T) db.Database {
	t.Helper()
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	require.NoError(t, database.Migrate(&Repo{}, &Branch{}, &PullRequest{}))
	t.Cleanup(func() { database.Close() })
	return database
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
		IsDraft:     false,
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

func TestPullRequest_Signals_Open(t *testing.T) {
	pr := &PullRequest{State: PRStateOpen, IsDraft: false}
	pr.ID = 1

	signals := pr.Signals()
	require.Len(t, signals, 1)
	require.Equal(t, common.SeverityInfo, signals[0].Severity)
	require.Equal(t, "open", signals[0].Label)
	require.Equal(t, "github", signals[0].Source)
}

func TestPullRequest_Signals_Merged(t *testing.T) {
	pr := &PullRequest{State: PRStateMerged}
	pr.ID = 2

	signals := pr.Signals()
	require.Len(t, signals, 1)
	require.Equal(t, common.SeverityInfo, signals[0].Severity)
	require.Equal(t, "merged", signals[0].Label)
	require.Equal(t, "github", signals[0].Source)
}

func TestPullRequest_Signals_Draft(t *testing.T) {
	pr := &PullRequest{State: PRStateOpen, IsDraft: true}
	pr.ID = 3

	signals := pr.Signals()
	require.Len(t, signals, 1)
	require.Equal(t, common.SeverityInfo, signals[0].Severity)
	require.Equal(t, "draft", signals[0].Label)
	require.Equal(t, "github", signals[0].Source)
}

func TestPullRequest_Signals_Closed(t *testing.T) {
	pr := &PullRequest{State: PRStateClosed}
	pr.ID = 4

	signals := pr.Signals()
	require.Empty(t, signals)
}

func TestPullRequest_StateAndDraftIndependent(t *testing.T) {
	openNotDraft := &PullRequest{State: PRStateOpen, IsDraft: false}
	openNotDraft.ID = 1
	signals := openNotDraft.Signals()
	require.Len(t, signals, 1)
	require.Equal(t, "open", signals[0].Label)

	openDraft := &PullRequest{State: PRStateOpen, IsDraft: true}
	openDraft.ID = 2
	signals = openDraft.Signals()
	require.Len(t, signals, 1)
	require.Equal(t, "draft", signals[0].Label)

	mergedDraft := &PullRequest{State: PRStateMerged, IsDraft: true}
	mergedDraft.ID = 3
	signals = mergedDraft.Signals()
	require.Len(t, signals, 1)
	require.Equal(t, "merged", signals[0].Label)

	closedDraft := &PullRequest{State: PRStateClosed, IsDraft: true}
	closedDraft.ID = 4
	signals = closedDraft.Signals()
	require.Empty(t, signals)
}
