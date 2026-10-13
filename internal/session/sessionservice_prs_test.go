package session

import (
	"context"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func countPRQueries(t *testing.T, database db.Database) *int {
	t.Helper()
	gormDB, ok := database.(*db.DB)
	require.True(t, ok)
	count := 0
	require.NoError(t, gormDB.Callback().Query().After("gorm:query").Register("test:count_pr_queries", func(tx *gorm.DB) {
		if tx.Statement.Table == "pull_requests" {
			count++
		}
	}))
	return &count
}

func prNumbers(prs []git.PullRequest) []int {
	out := make([]int, 0, len(prs))
	for _, pr := range prs {
		out = append(out, pr.Number)
	}
	return out
}

func TestListSessions_EmbedsPRsForEverySessionBranchInOneQuery(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()

	first := &Session{Name: "first", Status: StatusActive, LastUsedAt: time.Now()}
	require.NoError(t, env.sessionStore.Add(first))
	env.attachBranchWorktree(t, first.ID, env.branch.ID, 0)
	past := env.addHistoryBranch(t, first.ID, "first-base")

	secondBranch := &git.Branch{Name: "second-branch", RepoID: env.repo.ID}
	require.NoError(t, env.database.Create(secondBranch).Error)
	second := &Session{Name: "second", Status: StatusActive, LastUsedAt: time.Now().Add(-time.Minute)}
	require.NoError(t, env.sessionStore.Add(second))
	env.attachBranchWorktree(t, second.ID, secondBranch.ID, 0)

	unrelated := &git.Branch{Name: "unrelated", RepoID: env.repo.ID}
	require.NoError(t, env.database.Create(unrelated).Error)

	currentID, pastID, secondID, unrelatedID := env.branch.ID, past.ID, secondBranch.ID, unrelated.ID
	for _, pr := range []git.PullRequest{
		{RepoID: env.repo.ID, Number: 1, HeadBranchID: &currentID, State: git.PRStateOpen},
		{RepoID: env.repo.ID, Number: 2, HeadBranchID: &pastID, State: git.PRStateMerged},
		{RepoID: env.repo.ID, Number: 3, HeadBranchID: &secondID, State: git.PRStateDraft},
		{RepoID: env.repo.ID, Number: 4, HeadBranchID: &unrelatedID, State: git.PRStateOpen},
	} {
		require.NoError(t, env.database.Create(&pr).Error)
	}

	queries := countPRQueries(t, env.database)
	sessions, err := env.service.ListSessions(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, *queries, "PRs for all sessions must load in a single query")

	byName := make(map[string][]int, len(sessions))
	for _, sess := range sessions {
		byName[sess.Name] = prNumbers(sess.PullRequests)
	}
	require.ElementsMatch(t, []int{1, 2}, byName["first"])
	require.Equal(t, []int{3}, byName["second"])
}

func TestGetSession_EmbedsPRs(t *testing.T) {
	env := setupPRTestEnv(t)

	sess := &Session{Name: "solo", Status: StatusActive, LastUsedAt: time.Now()}
	require.NoError(t, env.sessionStore.Add(sess))
	env.attachBranchWorktree(t, sess.ID, env.branch.ID, 0)
	branchID := env.branch.ID
	require.NoError(t, env.database.Create(&git.PullRequest{RepoID: env.repo.ID, Number: 9, HeadBranchID: &branchID, State: git.PRStateOpen}).Error)

	got, err := env.service.GetSession(context.Background(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []int{9}, prNumbers(got.PullRequests))
}

func TestListSessions_NoBranchesSkipsPRQuery(t *testing.T) {
	env := setupPRTestEnv(t)
	require.NoError(t, env.sessionStore.Add(&Session{Name: "empty", Status: StatusActive, LastUsedAt: time.Now()}))

	queries := countPRQueries(t, env.database)
	sessions, err := env.service.ListSessions(context.Background())
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Empty(t, sessions[0].PullRequests)
	require.Equal(t, 0, *queries)
}
