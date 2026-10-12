package git

import (
	"context"
	"testing"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/require"
)

func TestGitService_DefaultBranch(t *testing.T) {
	clone, _ := initTestRepoWithOrigin(t)
	svc := NewGitService(testdb.New(t, &Repo{}, &Branch{}, &Worktree{}, &PullRequest{}))
	require.Equal(t, "main", svc.DefaultBranch(context.Background(), clone))
}

func TestGitService_RepointWorktree(t *testing.T) {
	database := testdb.New(t, &Repo{}, &Branch{}, &Worktree{}, &PullRequest{})
	svc := NewGitService(database)

	repo := &Repo{Path: "/tmp/repo", FullName: "o/r"}
	require.NoError(t, database.Create(repo).Error)
	oldBranch := &Branch{Name: "old", RepoID: repo.ID}
	newBranch := &Branch{Name: "new", RepoID: repo.ID}
	require.NoError(t, database.Create(oldBranch).Error)
	require.NoError(t, database.Create(newBranch).Error)
	wt := &Worktree{Path: "/tmp/wt", BranchID: oldBranch.ID, RepoID: repo.ID, Status: WorktreeStatusPresent}
	require.NoError(t, database.Create(wt).Error)

	loaded, err := svc.worktreeStore.GetByID(wt.ID)
	require.NoError(t, err)
	loaded.Branch = oldBranch

	require.NoError(t, svc.RepointWorktree(loaded, newBranch))

	reloaded, err := svc.worktreeStore.GetByID(wt.ID)
	require.NoError(t, err)
	require.Equal(t, newBranch.ID, reloaded.BranchID)
}

func TestGitService_RepointWorktree_DoesNotClobberOtherColumns(t *testing.T) {
	database := testdb.New(t, &Repo{}, &Branch{}, &Worktree{}, &PullRequest{})
	svc := NewGitService(database)

	repo := &Repo{Path: "/tmp/repo", FullName: "o/r"}
	require.NoError(t, database.Create(repo).Error)
	oldBranch := &Branch{Name: "old", RepoID: repo.ID}
	newBranch := &Branch{Name: "new", RepoID: repo.ID}
	require.NoError(t, database.Create(oldBranch).Error)
	require.NoError(t, database.Create(newBranch).Error)
	wt := &Worktree{Path: "/tmp/wt", BranchID: oldBranch.ID, RepoID: repo.ID, Status: WorktreeStatusPresent}
	require.NoError(t, database.Create(wt).Error)

	stale, err := svc.worktreeStore.GetByID(wt.ID)
	require.NoError(t, err)
	require.NoError(t, database.Model(&Worktree{}).Where("id = ?", wt.ID).Update("status", WorktreeStatusMissing).Error)

	require.NoError(t, svc.RepointWorktree(stale, newBranch))

	reloaded, err := svc.worktreeStore.GetByID(wt.ID)
	require.NoError(t, err)
	require.Equal(t, newBranch.ID, reloaded.BranchID)
	require.Equal(t, WorktreeStatusMissing, reloaded.Status)
	require.Equal(t, newBranch.ID, stale.BranchID)
	require.Equal(t, newBranch, stale.Branch)
}
