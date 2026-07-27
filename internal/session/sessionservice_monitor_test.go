package session

import (
	"context"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
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
	require.Equal(t, notificationTypePullRequest, notification.Type)
	require.Equal(t, prNotification{
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
	require.Equal(t, notificationTypePullRequest, snapshot[0].Type)
	require.Equal(t, prNotification{
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
	require.Equal(t, "draft", snapshot[0].Data.(prNotification).State)
}

func TestSessionSnapshot_UnknownSession(t *testing.T) {
	env := setupPRTestEnv(t)

	require.Empty(t, env.service.SessionSnapshot(context.Background(), 9999))
}
