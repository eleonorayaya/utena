package tmux

import (
	"errors"
	"testing"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) db.Database {
	return testdb.New(t, &TmuxSession{})
}

func TestAddAndGetByID(t *testing.T) {
	database := setupTestDB(t)
	store := NewTmuxStore(database)

	session := &TmuxSession{
		Name:     "dev",
		StartDir: "/home/user/dev",
		Env:      map[string]string{"TERM": "xterm"},
		Status:   TmuxStatusActive,
	}
	require.NoError(t, store.Add(session))
	assert.NotZero(t, session.ID)

	got, err := store.GetByID(session.ID)
	require.NoError(t, err)
	assert.Equal(t, "dev", got.Name)
	assert.Equal(t, "/home/user/dev", got.StartDir)
	assert.Equal(t, TmuxStatusActive, got.Status)
	assert.Equal(t, map[string]string{"TERM": "xterm"}, got.Env)
}

func TestGetByName(t *testing.T) {
	database := setupTestDB(t)
	store := NewTmuxStore(database)

	require.NoError(t, store.Add(&TmuxSession{Name: "alpha", StartDir: "/alpha"}))
	require.NoError(t, store.Add(&TmuxSession{Name: "beta", StartDir: "/beta"}))

	got, err := store.GetByName("beta")
	require.NoError(t, err)
	assert.Equal(t, "beta", got.Name)
	assert.Equal(t, "/beta", got.StartDir)
}

func TestNameUniqueness(t *testing.T) {
	database := setupTestDB(t)
	store := NewTmuxStore(database)

	require.NoError(t, store.Add(&TmuxSession{Name: "unique"}))
	err := store.Add(&TmuxSession{Name: "unique"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTmuxSessionAlreadyExists))

	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, common.CategoryConflict, appErr.Category)
	assert.Contains(t, err.Error(), "tmux session")
	assert.Contains(t, err.Error(), `"unique"`)
}

func TestUpdateNameUniqueness(t *testing.T) {
	database := setupTestDB(t)
	store := NewTmuxStore(database)

	require.NoError(t, store.Add(&TmuxSession{Name: "alpha"}))
	beta := &TmuxSession{Name: "beta"}
	require.NoError(t, store.Add(beta))

	beta.Name = "alpha"
	err := store.Update(beta)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTmuxSessionAlreadyExists))

	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, common.CategoryConflict, appErr.Category)
}

func TestUpdateStatus(t *testing.T) {
	database := setupTestDB(t)
	store := NewTmuxStore(database)

	session := &TmuxSession{Name: "sess", Status: TmuxStatusInactive}
	require.NoError(t, store.Add(session))

	session.Status = TmuxStatusActive
	require.NoError(t, store.Update(session))

	got, err := store.GetByID(session.ID)
	require.NoError(t, err)
	assert.Equal(t, TmuxStatusActive, got.Status)
}

func TestEnvJSONRoundTrip(t *testing.T) {
	database := setupTestDB(t)
	store := NewTmuxStore(database)

	env := map[string]string{"FOO": "bar", "BAZ": "qux"}
	session := &TmuxSession{Name: "envtest", Env: env}
	require.NoError(t, store.Add(session))

	got, err := store.GetByID(session.ID)
	require.NoError(t, err)
	assert.Equal(t, env, got.Env)
}

func TestDelete(t *testing.T) {
	database := setupTestDB(t)
	store := NewTmuxStore(database)

	session := &TmuxSession{Name: "todelete"}
	require.NoError(t, store.Add(session))

	require.NoError(t, store.Delete(session.ID))

	_, err := store.GetByID(session.ID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTmuxSessionNotFound))
}

func TestList(t *testing.T) {
	database := setupTestDB(t)
	store := NewTmuxStore(database)

	require.NoError(t, store.Add(&TmuxSession{Name: "one"}))
	require.NoError(t, store.Add(&TmuxSession{Name: "two"}))
	require.NoError(t, store.Add(&TmuxSession{Name: "three"}))

	list := store.List()
	assert.Len(t, list, 3)
}
