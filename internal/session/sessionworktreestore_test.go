package session

import (
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/require"
)

func TestSessionWorktreeStore_Add_Duplicate(t *testing.T) {
	database := testdb.New(t,
		&workspace.Workspace{},
		&git.Repo{},
		&git.Branch{},
		&git.Worktree{},
		&Session{},
		&SessionWorktree{},
	)

	repo := &git.Repo{Path: "/tmp/swt-test", FullName: "owner/swt"}
	require.NoError(t, database.Create(repo).Error)
	branch := &git.Branch{Name: "main", RepoID: repo.ID, ExistsLocal: true}
	require.NoError(t, database.Create(branch).Error)
	wt := &git.Worktree{Path: "/tmp/swt-test/.worktrees/main", BranchID: branch.ID, RepoID: repo.ID, Status: git.WorktreeStatusPending}
	require.NoError(t, database.Create(wt).Error)
	sess := &Session{Name: "swt-test", Status: StatusActive, LastUsedAt: time.Now()}
	require.NoError(t, database.Create(sess).Error)

	store := NewSessionWorktreeStore(database)

	require.NoError(t, store.Add(&SessionWorktree{SessionID: sess.ID, WorktreeID: wt.ID, Position: 0}))

	err := store.Add(&SessionWorktree{SessionID: sess.ID, WorktreeID: wt.ID, Position: 1})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrSessionWorktreeAlreadyExists)

	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, common.CategoryConflict, appErr.Category)
	require.Contains(t, err.Error(), "session")
	require.Contains(t, err.Error(), "worktree")
}
