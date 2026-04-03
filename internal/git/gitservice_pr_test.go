package git

import (
	"context"
	"testing"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockGitHubClient struct {
	repoPRs     []RawPR
	assignedPRs []RawPR
	prByNumber  map[int]*RawPR
	diffContent string
	currentUser string
	err         error
}

func (m *mockGitHubClient) ListRepoPRs(ctx context.Context, owner, repo string) ([]RawPR, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.repoPRs, nil
}

func (m *mockGitHubClient) ListAssignedPRs(ctx context.Context) ([]RawPR, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.assignedPRs, nil
}

func (m *mockGitHubClient) GetPR(ctx context.Context, owner, repo string, number int) (*RawPR, error) {
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

func makeRawPR(number int, title, headRef, state string, draft bool) RawPR {
	raw := RawPR{
		Number:  number,
		Title:   title,
		State:   state,
		Draft:   draft,
		HTMLURL: "https://github.com/owner/repo/pull/1",
	}
	raw.User.Login = "octocat"
	raw.Head.Ref = headRef
	raw.Head.Repo = &struct {
		FullName string `json:"full_name"`
	}{FullName: "owner/repo"}
	return raw
}

func TestSyncRepoPRs_CreatesNewPRs(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	ghClient := &mockGitHubClient{
		repoPRs: []RawPR{makeRawPR(1, "First PR", "feature-a", "open", false)},
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
		repoPRs: []RawPR{makeRawPR(1, "New Title", "feature-a", "open", false)},
	}
	svc := NewGitService(database, WithGitHubClient(ghClient))

	err := svc.SyncRepoPRs(context.Background(), repo)
	require.NoError(t, err)

	updated, err := prStore.GetByRepoAndNumber(repo.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, "New Title", updated.Title)
}

func TestSyncRepoPRs_PublishesStateChangedEvent(t *testing.T) {
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

	mergedAt := "2026-01-01T00:00:00Z"
	raw := makeRawPR(1, "Some PR", "feature-a", "closed", false)
	raw.MergedAt = &mergedAt

	ghClient := &mockGitHubClient{repoPRs: []RawPR{raw}}
	bus := &mockEventBus{}
	svc := NewGitService(database, WithGitHubClient(ghClient), WithEventBus(bus))

	err := svc.SyncRepoPRs(context.Background(), repo)
	require.NoError(t, err)

	require.Len(t, bus.events, 1)
	assert.Equal(t, EventPRStateChanged, bus.events[0].Type)
	data := bus.events[0].Data.(PRStateChangedEvent)
	assert.Equal(t, PRStateOpen, data.OldState)
	assert.Equal(t, PRStateMerged, data.NewState)
}

func TestSyncRepoPRs_PublishesPRDiscoveredEvent(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	ghClient := &mockGitHubClient{
		repoPRs: []RawPR{makeRawPR(1, "New PR", "feature-a", "open", false)},
	}
	bus := &mockEventBus{}
	svc := NewGitService(database, WithGitHubClient(ghClient), WithEventBus(bus))

	err := svc.SyncRepoPRs(context.Background(), repo)
	require.NoError(t, err)

	require.Len(t, bus.events, 1)
	assert.Equal(t, EventPRDiscovered, bus.events[0].Type)
	data := bus.events[0].Data.(PRDiscoveredEvent)
	assert.Equal(t, "New PR", data.PullRequest.Title)
	assert.Equal(t, repo.ID, data.Repo.ID)
}

func TestSyncRepoPRs_CreatesBranchForUnknownHead(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	ghClient := &mockGitHubClient{
		repoPRs: []RawPR{makeRawPR(1, "PR", "new-branch", "open", false)},
	}
	svc := NewGitService(database, WithGitHubClient(ghClient))

	err := svc.SyncRepoPRs(context.Background(), repo)
	require.NoError(t, err)

	branch, err := svc.branchStore.GetByNameAndRepo("new-branch", repo.ID)
	require.NoError(t, err)
	assert.True(t, branch.ExistsRemote)
}

func TestSyncAssignedPRs_ReturnsNewlyDiscoveredPRs(t *testing.T) {
	database, _ := setupGitServiceTest(t)
	ghClient := &mockGitHubClient{
		assignedPRs: []RawPR{makeRawPR(10, "Assigned PR", "feature-x", "open", false)},
	}
	svc := NewGitService(database, WithGitHubClient(ghClient))

	discovered, err := svc.SyncAssignedPRs(context.Background())
	require.NoError(t, err)
	require.Len(t, discovered, 1)
	assert.Equal(t, 10, discovered[0].Number)
	assert.Equal(t, "Assigned PR", discovered[0].Title)
}

func TestSyncAssignedPRs_SkipsAlreadyKnownPRs(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	branchStore := NewBranchStore(database)
	branch := &Branch{Name: "feature-x", RepoID: repo.ID}
	require.NoError(t, branchStore.Upsert(branch))

	prStore := NewPRStore(database)
	require.NoError(t, prStore.Add(&PullRequest{
		RepoID:       repo.ID,
		Number:       10,
		HeadBranchID: &branch.ID,
		Title:        "Already Known",
		State:        PRStateOpen,
		AuthorLogin:  "octocat",
	}))

	ghClient := &mockGitHubClient{
		assignedPRs: []RawPR{makeRawPR(10, "Already Known", "feature-x", "open", false)},
	}
	svc := NewGitService(database, WithGitHubClient(ghClient))

	discovered, err := svc.SyncAssignedPRs(context.Background())
	require.NoError(t, err)
	assert.Empty(t, discovered)
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

func TestSyncRepoPRs_NilGitHubClient_ReturnsNoError(t *testing.T) {
	database, repo := setupGitServiceTest(t)
	svc := NewGitService(database)

	err := svc.SyncRepoPRs(context.Background(), repo)
	assert.NoError(t, err)
}

func TestSyncAssignedPRs_NilGitHubClient_ReturnsEmptyResult(t *testing.T) {
	database, _ := setupGitServiceTest(t)
	svc := NewGitService(database)

	discovered, err := svc.SyncAssignedPRs(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, discovered)
}
