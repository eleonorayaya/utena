package session

import (
	"context"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/google/go-github/v72/github"
	"github.com/stretchr/testify/require"
)

func collectNotifications(env *prTestEnv) func() []eventbus.SessionNotificationEvent {
	var got []eventbus.SessionNotificationEvent
	env.bus.Subscribe(eventbus.SessionNotification, func(_ context.Context, event eventbus.Event) error {
		if data, ok := event.Data.(eventbus.SessionNotificationEvent); ok {
			got = append(got, data)
		}
		return nil
	})
	return func() []eventbus.SessionNotificationEvent { return got }
}

func TestHandlePRUpdated_PublishesSessionNotification(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()
	got := collectNotifications(env)

	branchID := env.branch.ID
	sess := &Session{Name: "feature-pr", Status: StatusActive, LastUsedAt: time.Now()}
	require.NoError(t, env.sessionStore.Add(sess))
	env.attachBranchWorktree(t, sess.ID, branchID, 0)

	event := eventbus.Event{
		Type: git.EventPRUpdated,
		Data: git.PRUpdatedEvent{
			PullRequest: &git.PullRequest{
				Number:       42,
				HeadBranchID: &branchID,
				Title:        "Test PR",
				State:        git.PRStateClosed,
				HTMLURL:      "https://github.com/eleonorayaya/utena/pull/42",
			},
			Previous: &git.PullRequest{State: git.PRStateOpen},
			Repo:     env.repo,
		},
	}
	require.NoError(t, env.service.handlePRUpdated(ctx, event))

	require.Len(t, got(), 1)
	notification := got()[0]
	require.Equal(t, sess.ID, notification.SessionID)
	require.Equal(t, git.NotificationPullRequest, notification.Type)
	require.Equal(t, git.PRNotification{
		Number:        42,
		Title:         "Test PR",
		State:         "closed",
		PreviousState: "open",
		Branch:        "feature-pr",
		URL:           "https://github.com/eleonorayaya/utena/pull/42",
	}, notification.Data)
}

func TestHandlePRUpdated_NoSessionForBranch_PublishesNothing(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()
	got := collectNotifications(env)

	branchID := env.branch.ID
	event := eventbus.Event{
		Type: git.EventPRUpdated,
		Data: git.PRUpdatedEvent{
			PullRequest: &git.PullRequest{Number: 42, HeadBranchID: &branchID, State: git.PRStateClosed},
			Previous:    &git.PullRequest{State: git.PRStateOpen},
			Repo:        env.repo,
		},
	}
	require.NoError(t, env.service.handlePRUpdated(ctx, event))

	require.Empty(t, got())
}

func TestSyncPRActivity_FansOutToEverySessionOnTheBranch(t *testing.T) {
	client := &git.MockGitHubClient{Reviews: []*github.PullRequestReview{{
		ID:    github.Ptr(int64(101)),
		State: github.Ptr("CHANGES_REQUESTED"),
		Body:  github.Ptr("please fix"),
		User:  &github.User{Login: github.Ptr("reviewer"), Type: github.Ptr("User")},
	}}}
	env := setupPRTestEnv(t, git.WithGitHubClient(client))
	got := collectNotifications(env)

	// worktrees.branch_id is unique, so two sessions share a branch by sharing
	// the worktree row
	branchID := env.branch.ID
	first := &Session{Name: "first", Status: StatusActive, LastUsedAt: time.Now()}
	require.NoError(t, env.sessionStore.Add(first))
	worktree := env.attachBranchWorktree(t, first.ID, branchID, 0)

	second := &Session{Name: "second", Status: StatusActive, LastUsedAt: time.Now()}
	require.NoError(t, env.sessionStore.Add(second))
	require.NoError(t, env.swtStore.Add(&SessionWorktree{SessionID: second.ID, WorktreeID: worktree.ID}))
	require.NoError(t, env.database.Create(&git.PullRequest{
		RepoID:            env.repo.ID,
		Number:            9,
		HeadBranchID:      &branchID,
		State:             git.PRStateOpen,
		HeadSHA:           "sha1",
		ActivityBaselined: true,
		LastReviewID:      100,
	}).Error)

	require.NoError(t, env.service.SyncPRActivity(context.Background()))

	// the fake client returns one review newer than the watermark
	require.Len(t, got(), 2, "both sessions on the branch must be notified")
	sessionIDs := []uint{got()[0].SessionID, got()[1].SessionID}
	require.NotEqual(t, sessionIDs[0], sessionIDs[1])
	for _, n := range got() {
		require.Equal(t, git.NotificationReview, n.Type)
	}
}

func TestSessionSnapshot_ReturnsSessionPRs(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	sess := &Session{Name: "feature-pr", Status: StatusActive, LastUsedAt: time.Now()}
	require.NoError(t, env.sessionStore.Add(sess))
	env.attachBranchWorktree(t, sess.ID, branchID, 0)
	require.NoError(t, env.database.Create(&git.PullRequest{
		RepoID:       env.repo.ID,
		Number:       7,
		HeadBranchID: &branchID,
		Title:        "Snapshot PR",
		State:        git.PRStateOpen,
		HTMLURL:      "https://github.com/eleonorayaya/utena/pull/7",
	}).Error)

	snapshot := env.service.SessionSnapshot(ctx, sess.ID)

	require.Len(t, snapshot, 1)
	require.Equal(t, sess.ID, snapshot[0].SessionID)
	require.Equal(t, git.NotificationPullRequest, snapshot[0].Type)
	require.Equal(t, git.PRNotification{
		Number: 7,
		Title:  "Snapshot PR",
		State:  "open",
		Branch: "feature-pr",
		URL:    "https://github.com/eleonorayaya/utena/pull/7",
	}, snapshot[0].Data)
}

func TestSessionSnapshot_SkipsClosedAndMergedPRs(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	branchID := env.branch.ID
	sess := &Session{Name: "feature-pr", Status: StatusActive, LastUsedAt: time.Now()}
	require.NoError(t, env.sessionStore.Add(sess))
	env.attachBranchWorktree(t, sess.ID, branchID, 0)
	for i, state := range []git.PRState{git.PRStateMerged, git.PRStateClosed, git.PRStateDraft} {
		require.NoError(t, env.database.Create(&git.PullRequest{
			RepoID:       env.repo.ID,
			Number:       100 + i,
			HeadBranchID: &branchID,
			State:        state,
		}).Error)
	}

	snapshot := env.service.SessionSnapshot(ctx, sess.ID)

	require.Len(t, snapshot, 1)
	require.Equal(t, "draft", snapshot[0].Data.(git.PRNotification).State)
}

func TestSessionSnapshot_UnknownSession(t *testing.T) {
	env := setupPRTestEnv(t)

	require.Empty(t, env.service.SessionSnapshot(context.Background(), 9999))
}

func (env *prTestEnv) addHistoryBranch(t *testing.T, sessionID uint, name string) *git.Branch {
	t.Helper()
	branch := &git.Branch{Name: name, RepoID: env.repo.ID}
	require.NoError(t, env.database.Create(branch).Error)
	require.NoError(t, NewSessionBranchStore(env.database).Add(&SessionBranch{SessionID: sessionID, BranchID: branch.ID, FirstSeenAt: time.Now()}))
	return branch
}

func TestHandlePRUpdated_RoutesPRFromHistoryBranch(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()
	got := collectNotifications(env)

	sess := &Session{Name: "stacked", Status: StatusActive, LastUsedAt: time.Now()}
	require.NoError(t, env.sessionStore.Add(sess))
	env.attachBranchWorktree(t, sess.ID, env.branch.ID, 0)
	past := env.addHistoryBranch(t, sess.ID, "stacked-base")
	pastID := past.ID

	require.NoError(t, env.service.handlePRUpdated(ctx, eventbus.Event{
		Type: git.EventPRUpdated,
		Data: git.PRUpdatedEvent{
			PullRequest: &git.PullRequest{Number: 5, HeadBranchID: &pastID, Title: "Base", State: git.PRStateOpen},
			Previous:    &git.PullRequest{State: git.PRStateDraft},
			Repo:        env.repo,
		},
	}))

	require.Len(t, got(), 1)
	require.Equal(t, sess.ID, got()[0].SessionID)
	require.Equal(t, "stacked-base", got()[0].Data.(git.PRNotification).Branch)
}

func TestSessionSnapshot_IncludesHistoryBranchPRs(t *testing.T) {
	env := setupPRTestEnv(t)
	sess := &Session{Name: "stacked", Status: StatusActive, LastUsedAt: time.Now()}
	require.NoError(t, env.sessionStore.Add(sess))
	env.attachBranchWorktree(t, sess.ID, env.branch.ID, 0)
	past := env.addHistoryBranch(t, sess.ID, "stacked-base")
	currentID, pastID := env.branch.ID, past.ID
	require.NoError(t, env.database.Create(&git.PullRequest{RepoID: env.repo.ID, Number: 1, HeadBranchID: &currentID, State: git.PRStateOpen}).Error)
	require.NoError(t, env.database.Create(&git.PullRequest{RepoID: env.repo.ID, Number: 2, HeadBranchID: &pastID, State: git.PRStateOpen}).Error)

	snapshot := env.service.SessionSnapshot(context.Background(), sess.ID)

	require.Len(t, snapshot, 2)
	branches := []string{snapshot[0].Data.(git.PRNotification).Branch, snapshot[1].Data.(git.PRNotification).Branch}
	require.ElementsMatch(t, []string{"feature-pr", "stacked-base"}, branches)
}

func TestSyncPRActivity_CoversHistoryBranches(t *testing.T) {
	client := &git.MockGitHubClient{Reviews: []*github.PullRequestReview{{
		ID:    github.Ptr(int64(101)),
		State: github.Ptr("APPROVED"),
		User:  &github.User{Login: github.Ptr("reviewer"), Type: github.Ptr("User")},
	}}}
	env := setupPRTestEnv(t, git.WithGitHubClient(client))
	got := collectNotifications(env)

	sess := &Session{Name: "stacked", Status: StatusActive, LastUsedAt: time.Now()}
	require.NoError(t, env.sessionStore.Add(sess))
	env.attachBranchWorktree(t, sess.ID, env.branch.ID, 0)
	past := env.addHistoryBranch(t, sess.ID, "stacked-base")
	pastID := past.ID
	require.NoError(t, env.database.Create(&git.PullRequest{
		RepoID: env.repo.ID, Number: 2, HeadBranchID: &pastID, State: git.PRStateOpen,
		HeadSHA: "sha1", ActivityBaselined: true, LastReviewID: 100,
	}).Error)

	require.NoError(t, env.service.SyncPRActivity(context.Background()))

	require.Len(t, got(), 1)
	require.Equal(t, sess.ID, got()[0].SessionID)
	require.Equal(t, git.NotificationReview, got()[0].Type)
}
