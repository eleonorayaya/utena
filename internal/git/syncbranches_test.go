package git

import (
	"context"
	"testing"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/require"
)

func TestGitService_SyncBranches_MissingRepoPathReturnsErrorEarly(t *testing.T) {
	database := testdb.New(t, &Repo{}, &Branch{})
	svc := NewGitService(database)
	repoStore := NewRepoStore(database)
	branchStore := NewBranchStore(database)

	repo := &Repo{Path: "/nonexistent/path/repo", FullName: "owner/repo"}
	require.NoError(t, repoStore.Add(repo))
	require.NoError(t, branchStore.Add(&Branch{Name: "main", RepoID: repo.ID}))
	require.NoError(t, branchStore.Add(&Branch{Name: "feature", RepoID: repo.ID}))

	err := svc.SyncBranches(context.Background(), repo.ID, repo.Path)
	require.Error(t, err, "syncing a repo whose path no longer exists should error once, not per-branch")
	require.Contains(t, err.Error(), "repo path")
}
