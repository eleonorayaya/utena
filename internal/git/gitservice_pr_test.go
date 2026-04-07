package git

import (
	"context"
	"testing"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/google/go-github/v72/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockGitHubClient struct {
	repoPRs     []*github.PullRequest
	prByNumber  map[int]*github.PullRequest
	diffContent string
	currentUser string
	err         error
}

func (m *mockGitHubClient) ListRepoPRs(ctx context.Context, owner, repo string) ([]*github.PullRequest, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.repoPRs, nil
}

func (m *mockGitHubClient) GetPR(ctx context.Context, owner, repo string, number int) (*github.PullRequest, error) {
	if m.err != nil {
		return nil, m.err
	}
	if pr, ok := m.prByNumber[number]; ok {
		return pr, nil
	}
	return nil, ErrPRNotFound
}

func (m *mockGitHubClient) GetPRDiff(ctx context.Context, owner, repo string, number int) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.diffContent, nil
}

func (m *mockGitHubClient) GetCurrentUser(ctx context.Context) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.currentUser, nil
}

type mockEventBus struct {
	events []eventbus.Event
}

func (m *mockEventBus) Publish(ctx context.Context, event eventbus.Event) error {
	m.events = append(m.events, event)
	return nil
}

func (m *mockEventBus) Subscribe(eventType string, handler eventbus.Handler) {}

func setupGitServiceTest(t *testing.T) (db.Database, *Repo) {
	t.Helper()
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	require.NoError(t, database.Migrate(&Repo{}, &Branch{}, &Worktree{}, &PullRequest{}))
	t.Cleanup(func() { database.Close() })

	repoStore := NewRepoStore(database)
	repo := &Repo{Path: "/test/repo", FullName: "owner/repo"}
	require.NoError(t, repoStore.Add(repo))
	return database, repo
}

func makeGitHubPR(number int, title, headRef, state string, draft bool) *github.PullRequest {
	return &github.PullRequest{
		Number:  github.Ptr(number),
		Title:   github.Ptr(title),
		State:   github.Ptr(state),
		Draft:   github.Ptr(draft),
		HTMLURL: github.Ptr("https://github.com/owner/repo/pull/1"),
		User:    &github.User{Login: github.Ptr("octocat")},
		Head: &github.PullRequestBranch{
			Ref:  github.Ptr(headRef),
			Repo: &github.Repository{FullName: github.Ptr("owner/repo")},
		},
		Base: &github.PullRequestBranch{Ref: github.Ptr("main")},
	}
}

func TestSyncRepoPRs_CreatesNewPRs(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	ghClient := &mockGitHubClient{
		repoPRs: []*github.PullRequest{makeGitHubPR(1, "First PR", "feature-a", "open", false)},
	}
	svc := NewGitService(database, WithGitHubClient(ghClient))

	err := svc.SyncRepoPRs(context.Background(), repo)
	require.NoError(t, err)

	prs := svc.prStore.ListByRepo(repo.ID)
	require.Len(t, prs, 1)
	assert.Equal(t, 1, prs[0].Number)
	assert.Equal(t, "First PR", prs[0].Title)
	assert.Equal(t, PRStateOpen, prs[0].State)
}

func TestSyncRepoPRs_UpdatesExistingPRs(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	branchStore := NewBranchStore(database)
	branch := &Branch{Name: "feature-a", RepoID: repo.ID}
	require.NoError(t, branchStore.Upsert(branch))

	prStore := NewPRStore(database)
	existing := &PullRequest{
		RepoID:       repo.ID,
		Number:       1,
		HeadBranchID: &branch.ID,
		Title:        "Old Title",
		State:        PRStateOpen,
		AuthorLogin:  "octocat",
	}
	require.NoError(t, prStore.Add(existing))

	ghClient := &mockGitHubClient{
		repoPRs: []*github.PullRequest{makeGitHubPR(1, "New Title", "feature-a", "open", false)},
	}
	svc := NewGitService(database, WithGitHubClient(ghClient))

	err := svc.SyncRepoPRs(context.Background(), repo)
	require.NoError(t, err)

	updated, err := prStore.GetByRepoAndNumber(repo.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, "New Title", updated.Title)
}

func TestSyncRepoPRs_PublishesPRUpdatedOnStateChange(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	branchStore := NewBranchStore(database)
	branch := &Branch{Name: "feature-a", RepoID: repo.ID}
	require.NoError(t, branchStore.Upsert(branch))

	prStore := NewPRStore(database)
	existing := &PullRequest{
		RepoID:       repo.ID,
		Number:       1,
		HeadBranchID: &branch.ID,
		Title:        "Some PR",
		State:        PRStateOpen,
		AuthorLogin:  "octocat",
	}
	require.NoError(t, prStore.Add(existing))

	mergedAt := github.Timestamp{}
	raw := makeGitHubPR(1, "Some PR", "feature-a", "closed", false)
	raw.MergedAt = &mergedAt

	ghClient := &mockGitHubClient{repoPRs: []*github.PullRequest{raw}}
	bus := &mockEventBus{}
	svc := NewGitService(database, WithGitHubClient(ghClient), WithEventBus(bus))

	err := svc.SyncRepoPRs(context.Background(), repo)
	require.NoError(t, err)

	require.Len(t, bus.events, 1)
	assert.Equal(t, EventPRUpdated, bus.events[0].Type)
	data := bus.events[0].Data.(PRUpdatedEvent)
	require.NotNil(t, data.Previous)
	assert.Equal(t, PRStateOpen, data.Previous.State)
	assert.Equal(t, PRStateMerged, data.PullRequest.State)
}

func TestSyncRepoPRs_PublishesPRUpdatedForNewPR(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	ghClient := &mockGitHubClient{
		repoPRs: []*github.PullRequest{makeGitHubPR(1, "New PR", "feature-a", "open", false)},
	}
	bus := &mockEventBus{}
	svc := NewGitService(database, WithGitHubClient(ghClient), WithEventBus(bus))

	err := svc.SyncRepoPRs(context.Background(), repo)
	require.NoError(t, err)

	require.Len(t, bus.events, 1)
	assert.Equal(t, EventPRUpdated, bus.events[0].Type)
	data := bus.events[0].Data.(PRUpdatedEvent)
	assert.Nil(t, data.Previous)
	assert.Equal(t, "New PR", data.PullRequest.Title)
	assert.Equal(t, repo.ID, data.Repo.ID)
}

func TestSyncRepoPRs_CreatesBranchForUnknownHead(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	ghClient := &mockGitHubClient{
		repoPRs: []*github.PullRequest{makeGitHubPR(1, "PR", "new-branch", "open", false)},
	}
	svc := NewGitService(database, WithGitHubClient(ghClient))

	err := svc.SyncRepoPRs(context.Background(), repo)
	require.NoError(t, err)

	branch, err := svc.branchStore.GetByNameAndRepo("new-branch", repo.ID)
	require.NoError(t, err)
	assert.True(t, branch.ExistsRemote)
}

func TestSearchPRs_FiltersByRepoAndState(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	branchStore := NewBranchStore(database)
	branch := &Branch{Name: "feature", RepoID: repo.ID}
	require.NoError(t, branchStore.Upsert(branch))

	prStore := NewPRStore(database)
	require.NoError(t, prStore.Add(&PullRequest{
		RepoID: repo.ID, Number: 1, HeadBranchID: &branch.ID,
		Title: "Open PR", State: PRStateOpen, AuthorLogin: "a",
	}))
	require.NoError(t, prStore.Add(&PullRequest{
		RepoID: repo.ID, Number: 2, HeadBranchID: &branch.ID,
		Title: "Merged PR", State: PRStateMerged, AuthorLogin: "b",
	}))

	svc := NewGitService(database)

	open := svc.SearchPRs(repo.ID, PRStateOpen)
	assert.Len(t, open, 1)
	assert.Equal(t, "Open PR", open[0].Title)

	all := svc.SearchPRs(repo.ID, "")
	assert.Len(t, all, 2)
}

func TestGetPRsForBranch_ReturnsCorrectPRs(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	branchStore := NewBranchStore(database)
	branchA := &Branch{Name: "branch-a", RepoID: repo.ID}
	branchB := &Branch{Name: "branch-b", RepoID: repo.ID}
	require.NoError(t, branchStore.Upsert(branchA))
	require.NoError(t, branchStore.Upsert(branchB))

	prStore := NewPRStore(database)
	require.NoError(t, prStore.Add(&PullRequest{
		RepoID: repo.ID, Number: 1, HeadBranchID: &branchA.ID,
		Title: "PR for A", State: PRStateOpen, AuthorLogin: "a",
	}))
	require.NoError(t, prStore.Add(&PullRequest{
		RepoID: repo.ID, Number: 2, HeadBranchID: &branchB.ID,
		Title: "PR for B", State: PRStateOpen, AuthorLogin: "b",
	}))

	svc := NewGitService(database)
	prs := svc.GetPRsForBranch(branchA.ID)
	require.Len(t, prs, 1)
	assert.Equal(t, "PR for A", prs[0].Title)
}

func TestGetPRDiff_ReturnsStructuredDiff(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	branchStore := NewBranchStore(database)
	branch := &Branch{Name: "feature", RepoID: repo.ID}
	require.NoError(t, branchStore.Upsert(branch))

	prStore := NewPRStore(database)
	require.NoError(t, prStore.Add(&PullRequest{
		RepoID: repo.ID, Number: 1, HeadBranchID: &branch.ID,
		Title: "PR", State: PRStateOpen, AuthorLogin: "a",
	}))

	pr, _ := prStore.GetByRepoAndNumber(repo.ID, 1)
	diffText := "diff --git a/file.go b/file.go\n--- a/file.go\n+++ b/file.go\n@@ -1,3 +1,4 @@\n package main\n \n+import \"fmt\"\n func main() {}\n"

	ghClient := &mockGitHubClient{diffContent: diffText}
	svc := NewGitService(database, WithGitHubClient(ghClient))

	diff, err := svc.GetPRDiff(context.Background(), pr.ID)
	require.NoError(t, err)
	require.Len(t, diff.Files, 1)
	assert.Equal(t, "file.go", diff.Files[0].NewPath)
}

func TestSyncRepoPRs_NilGitHubClient_ReturnsError(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	svc := NewGitService(database)

	err := svc.SyncRepoPRs(context.Background(), repo)
	assert.ErrorIs(t, err, ErrNoGitHubClient)
}

func TestSyncRepoPRs_SetsIsAssignedToMe(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	raw := makeGitHubPR(1, "Assigned PR", "feature-a", "open", false)
	raw.Assignees = []*github.User{{Login: github.Ptr("myself")}}
	ghClient := &mockGitHubClient{repoPRs: []*github.PullRequest{raw}}
	svc := NewGitService(database, WithGitHubClient(ghClient))
	svc.currentUser = "myself"

	err := svc.SyncRepoPRs(context.Background(), repo)
	require.NoError(t, err)

	prs := svc.prStore.ListByRepo(repo.ID)
	require.Len(t, prs, 1)
	assert.True(t, prs[0].IsAssignedToMe)
}

func TestSyncRepoPRs_NotAssigned(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	raw := makeGitHubPR(1, "Someone elses PR", "feature-b", "open", false)
	raw.Assignees = []*github.User{{Login: github.Ptr("someone-else")}}
	ghClient := &mockGitHubClient{repoPRs: []*github.PullRequest{raw}}
	svc := NewGitService(database, WithGitHubClient(ghClient))
	svc.currentUser = "myself"

	err := svc.SyncRepoPRs(context.Background(), repo)
	require.NoError(t, err)

	prs := svc.prStore.ListByRepo(repo.ID)
	require.Len(t, prs, 1)
	assert.False(t, prs[0].IsAssignedToMe)
}
