package git

import (
	"context"
	"testing"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/google/go-github/v72/github"
	"github.com/stretchr/testify/require"
)

func activityEnv(t *testing.T, client *mockGitHubClient) (*GitService, *PullRequest) {
	t.Helper()

	database := testdb.New(t, &Repo{}, &Branch{}, &Worktree{}, &PullRequest{})
	service := NewGitService(database, WithGitHubClient(client))
	service.currentUser = "eleonorayaya"

	repo := &Repo{Path: "/tmp/repo", FullName: "eleonorayaya/utena"}
	require.NoError(t, database.Create(repo).Error)

	pr := &PullRequest{RepoID: repo.ID, Number: 7, Title: "Test PR", State: PRStateOpen, HeadSHA: "abc123"}
	require.NoError(t, database.Create(pr).Error)

	return service, pr
}

func review(id int64, login, userType, state string) *github.PullRequestReview {
	return &github.PullRequestReview{
		ID:    github.Ptr(id),
		State: github.Ptr(state),
		Body:  github.Ptr("looks good"),
		User:  &github.User{Login: github.Ptr(login), Type: github.Ptr(userType)},
	}
}

func checkRun(name, status, conclusion string) *github.CheckRun {
	return &github.CheckRun{
		Name:       github.Ptr(name),
		Status:     github.Ptr(status),
		Conclusion: github.Ptr(conclusion),
	}
}

func TestSyncPRActivity_FirstSyncOnlyBaselines(t *testing.T) {
	client := &mockGitHubClient{
		reviews:        []*github.PullRequestReview{review(10, "someone", "User", "APPROVED")},
		reviewComments: []*github.PullRequestComment{{ID: github.Ptr(int64(5)), User: &github.User{Login: github.Ptr("someone")}}},
	}
	service, pr := activityEnv(t, client)

	activity, err := service.SyncPRActivity(context.Background(), pr)
	require.NoError(t, err)

	require.Empty(t, activity, "existing activity must not be replayed on first sight")
	require.True(t, pr.ActivityBaselined)
	require.Equal(t, int64(10), pr.LastReviewID)
	require.Equal(t, int64(5), pr.LastReviewCommentID)
}

func TestSyncPRActivity_FirstReviewOnAPRThatHadNone(t *testing.T) {
	client := &mockGitHubClient{}
	service, pr := activityEnv(t, client)

	_, err := service.SyncPRActivity(context.Background(), pr)
	require.NoError(t, err)
	require.True(t, pr.ActivityBaselined)
	require.Zero(t, pr.LastReviewID, "nothing to watermark yet")

	client.reviews = []*github.PullRequestReview{review(500, "someone", "User", "CHANGES_REQUESTED")}
	activity, err := service.SyncPRActivity(context.Background(), pr)
	require.NoError(t, err)

	require.Len(t, activity, 1, "the first review on a PR must not be mistaken for a baseline")
	require.Equal(t, ActivityReview, activity[0].Type)
	require.Equal(t, int64(500), pr.LastReviewID)
}

func TestSyncPRActivity_NewReview(t *testing.T) {
	client := &mockGitHubClient{reviews: []*github.PullRequestReview{review(10, "someone", "User", "APPROVED")}}
	service, pr := activityEnv(t, client)
	pr.ActivityBaselined = true
	pr.LastReviewID = 9

	activity, err := service.SyncPRActivity(context.Background(), pr)
	require.NoError(t, err)

	require.Len(t, activity, 1)
	require.Equal(t, ActivityReview, activity[0].Type)
	require.Equal(t, ReviewActivity{
		Number: 7,
		Title:  "Test PR",
		State:  "approved",
		Author: "someone",
		Body:   "looks good",
	}, activity[0].Data)
	require.Equal(t, int64(10), pr.LastReviewID)
}

func TestSyncPRActivity_SkipsBotsAndSelf(t *testing.T) {
	client := &mockGitHubClient{reviews: []*github.PullRequestReview{
		review(10, "coverage-bot", "Bot", "COMMENTED"),
		review(11, "dependabot[bot]", "User", "COMMENTED"),
		review(12, "eleonorayaya", "User", "APPROVED"),
	}}
	service, pr := activityEnv(t, client)
	pr.ActivityBaselined = true
	pr.LastReviewID = 9

	activity, err := service.SyncPRActivity(context.Background(), pr)
	require.NoError(t, err)

	require.Empty(t, activity)
	require.Equal(t, int64(12), pr.LastReviewID, "watermark must still advance past filtered activity")
}

func TestSyncPRActivity_NewReviewComment(t *testing.T) {
	client := &mockGitHubClient{reviewComments: []*github.PullRequestComment{{
		ID:      github.Ptr(int64(20)),
		Path:    github.Ptr("internal/monitor/monitorservice.go"),
		Line:    github.Ptr(42),
		Body:    github.Ptr("this leaks"),
		HTMLURL: github.Ptr("https://github.com/eleonorayaya/utena/pull/7#discussion_r20"),
		User:    &github.User{Login: github.Ptr("someone"), Type: github.Ptr("User")},
	}}}
	service, pr := activityEnv(t, client)
	pr.ActivityBaselined = true
	pr.LastReviewCommentID = 19

	activity, err := service.SyncPRActivity(context.Background(), pr)
	require.NoError(t, err)

	require.Len(t, activity, 1)
	require.Equal(t, ActivityReviewComment, activity[0].Type)
	require.Equal(t, ReviewCommentActivity{
		Number: 7,
		Title:  "Test PR",
		Author: "someone",
		Path:   "internal/monitor/monitorservice.go",
		Line:   42,
		Body:   "this leaks",
		URL:    "https://github.com/eleonorayaya/utena/pull/7#discussion_r20",
	}, activity[0].Data)
}

func TestChecksTransitions(t *testing.T) {
	cases := []struct {
		name       string
		prev       ChecksState
		prevSHA    string
		runs       []*github.CheckRun
		wantEvent  bool
		wantState  ChecksState
		wantFailed []string
	}{
		{
			name:      "still running is silent",
			runs:      []*github.CheckRun{checkRun("build", "in_progress", "")},
			wantState: ChecksStatePending,
		},
		{
			name:       "first failure while others run",
			prev:       ChecksStatePending,
			prevSHA:    "abc123",
			runs:       []*github.CheckRun{checkRun("lint", "completed", "failure"), checkRun("build", "in_progress", "")},
			wantEvent:  true,
			wantState:  ChecksStateFailing,
			wantFailed: []string{"lint"},
		},
		{
			name:      "repeat failure is silent",
			prev:      ChecksStateFailing,
			prevSHA:   "abc123",
			runs:      []*github.CheckRun{checkRun("lint", "completed", "failure"), checkRun("build", "in_progress", "")},
			wantState: ChecksStateFailing,
		},
		{
			name:      "all green rollup",
			prev:      ChecksStatePending,
			prevSHA:   "abc123",
			runs:      []*github.CheckRun{checkRun("lint", "completed", "success"), checkRun("build", "completed", "skipped")},
			wantEvent: true,
			wantState: ChecksStatePassed,
		},
		{
			name:       "failed rollup after first failure",
			prev:       ChecksStateFailing,
			prevSHA:    "abc123",
			runs:       []*github.CheckRun{checkRun("lint", "completed", "failure"), checkRun("build", "completed", "timed_out")},
			wantEvent:  true,
			wantState:  ChecksStateFailed,
			wantFailed: []string{"lint", "build"},
		},
		{
			name:      "new commit resets without an event",
			prev:      ChecksStateFailed,
			prevSHA:   "old-sha",
			runs:      []*github.CheckRun{checkRun("lint", "queued", "")},
			wantState: ChecksStatePending,
		},
		{
			name:      "no checks configured is silent",
			prev:      ChecksStatePending,
			prevSHA:   "abc123",
			wantState: ChecksStatePending,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pr := &PullRequest{Number: 7, Title: "Test PR", HeadSHA: "abc123", ChecksState: tc.prev, ChecksHeadSHA: tc.prevSHA}

			event, state := checksTransition(pr, tc.runs)

			require.Equal(t, tc.wantState, state)
			if !tc.wantEvent {
				require.Nil(t, event)
				return
			}
			require.NotNil(t, event)
			require.Equal(t, ActivityChecks, event.Type)
			data := event.Data.(ChecksActivity)
			require.Equal(t, string(tc.wantState), data.State)
			require.Equal(t, tc.wantFailed, data.Failed)
		})
	}
}

func TestSyncPRActivity_PersistsWatermarks(t *testing.T) {
	client := &mockGitHubClient{
		reviews:   []*github.PullRequestReview{review(10, "someone", "User", "APPROVED")},
		checkRuns: []*github.CheckRun{checkRun("lint", "completed", "success")},
	}
	service, pr := activityEnv(t, client)

	_, err := service.SyncPRActivity(context.Background(), pr)
	require.NoError(t, err)

	stored, err := service.prStore.GetByID(pr.ID)
	require.NoError(t, err)
	require.Equal(t, int64(10), stored.LastReviewID)
	require.Equal(t, ChecksStatePassed, stored.ChecksState)
	require.Equal(t, "abc123", stored.ChecksHeadSHA)
}
