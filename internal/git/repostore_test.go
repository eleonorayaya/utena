package git

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) db.Database {
	return testdb.New(t, &Repo{})
}

func TestRepoStore_AddAndGetByID(t *testing.T) {
	store := NewRepoStore(setupTestDB(t))

	repo := &Repo{Path: "/home/user/project", FullName: "user/project"}
	require.NoError(t, store.Add(repo))
	require.NotZero(t, repo.ID)

	found, err := store.GetByID(repo.ID)
	require.NoError(t, err)
	require.Equal(t, repo.Path, found.Path)
	require.Equal(t, repo.FullName, found.FullName)
}

func TestRepoStore_GetByPath(t *testing.T) {
	store := NewRepoStore(setupTestDB(t))

	repo := &Repo{Path: "/home/user/project", FullName: "user/project"}
	require.NoError(t, store.Add(repo))

	found, err := store.GetByPath("/home/user/project")
	require.NoError(t, err)
	require.Equal(t, repo.ID, found.ID)
	require.Equal(t, "user/project", found.FullName)
}

func TestRepoStore_GetByFullName(t *testing.T) {
	store := NewRepoStore(setupTestDB(t))

	repo := &Repo{Path: "/home/user/project", FullName: "user/project"}
	require.NoError(t, store.Add(repo))

	found, err := store.GetByFullName("user/project")
	require.NoError(t, err)
	require.Equal(t, repo.ID, found.ID)
	require.Equal(t, "/home/user/project", found.Path)
}

func TestRepoStore_UpsertCreates(t *testing.T) {
	store := NewRepoStore(setupTestDB(t))

	repo := &Repo{Path: "/home/user/new-project", FullName: "user/new-project"}
	require.NoError(t, store.Upsert(repo))
	require.NotZero(t, repo.ID)

	found, err := store.GetByPath("/home/user/new-project")
	require.NoError(t, err)
	require.Equal(t, "user/new-project", found.FullName)
}

func TestRepoStore_UpsertUpdates(t *testing.T) {
	store := NewRepoStore(setupTestDB(t))

	repo := &Repo{Path: "/home/user/project", FullName: "user/project"}
	require.NoError(t, store.Add(repo))

	updated := &Repo{Path: "/home/user/project", FullName: "org/project"}
	require.NoError(t, store.Upsert(updated))

	found, err := store.GetByPath("/home/user/project")
	require.NoError(t, err)
	require.Equal(t, "org/project", found.FullName)
	require.Equal(t, repo.ID, found.ID)
}

func TestRepoStore_PathUniqueness(t *testing.T) {
	store := NewRepoStore(setupTestDB(t))

	repo1 := &Repo{Path: "/home/user/project", FullName: "user/project"}
	require.NoError(t, store.Add(repo1))

	repo2 := &Repo{Path: "/home/user/project", FullName: "other/project"}
	err := store.Add(repo2)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRepoAlreadyExists)

	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, common.CategoryConflict, appErr.Category)
	require.Contains(t, err.Error(), "repo")
	require.Contains(t, err.Error(), "/home/user/project")
}

func TestRepoStore_List(t *testing.T) {
	store := NewRepoStore(setupTestDB(t))

	require.NoError(t, store.Add(&Repo{Path: "/home/user/project1", FullName: "user/project1"}))
	require.NoError(t, store.Add(&Repo{Path: "/home/user/project2", FullName: "user/project2"}))
	require.NoError(t, store.Add(&Repo{Path: "/home/user/project3", FullName: "user/project3"}))

	repos := store.List()
	require.Len(t, repos, 3)
}
