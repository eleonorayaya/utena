package mux

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
	return testdb.New(t, &Session{})
}

func TestAddAndGetByID(t *testing.T) {
	database := setupTestDB(t)
	store := NewStore(database)

	session := &Session{
		Name:     "dev",
		StartDir: "/home/user/dev",
		Env:      map[string]string{"TERM": "xterm"},
		Status:   StatusActive,
	}
	require.NoError(t, store.Add(session))
	assert.NotZero(t, session.ID)

	got, err := store.GetByID(session.ID)
	require.NoError(t, err)
	assert.Equal(t, "dev", got.Name)
	assert.Equal(t, "/home/user/dev", got.StartDir)
	assert.Equal(t, StatusActive, got.Status)
	assert.Equal(t, map[string]string{"TERM": "xterm"}, got.Env)
}

func TestGetByName(t *testing.T) {
	database := setupTestDB(t)
	store := NewStore(database)

	require.NoError(t, store.Add(&Session{Name: "alpha", StartDir: "/alpha"}))
	require.NoError(t, store.Add(&Session{Name: "beta", StartDir: "/beta"}))

	got, err := store.GetByName("beta")
	require.NoError(t, err)
	assert.Equal(t, "beta", got.Name)
	assert.Equal(t, "/beta", got.StartDir)
}

func TestNameUniqueness(t *testing.T) {
	database := setupTestDB(t)
	store := NewStore(database)

	require.NoError(t, store.Add(&Session{Name: "unique"}))
	err := store.Add(&Session{Name: "unique"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSessionAlreadyExists))

	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, common.CategoryConflict, appErr.Category)
	assert.Contains(t, err.Error(), "mux session")
	assert.Contains(t, err.Error(), `"unique"`)
}

func TestUpdateNameUniqueness(t *testing.T) {
	database := setupTestDB(t)
	store := NewStore(database)

	require.NoError(t, store.Add(&Session{Name: "alpha"}))
	beta := &Session{Name: "beta"}
	require.NoError(t, store.Add(beta))

	beta.Name = "alpha"
	err := store.Update(beta)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSessionAlreadyExists))

	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, common.CategoryConflict, appErr.Category)
}

func TestUpdateStatus(t *testing.T) {
	database := setupTestDB(t)
	store := NewStore(database)

	session := &Session{Name: "sess", Status: StatusInactive}
	require.NoError(t, store.Add(session))

	session.Status = StatusActive
	require.NoError(t, store.Update(session))

	got, err := store.GetByID(session.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusActive, got.Status)
}

func TestEnvJSONRoundTrip(t *testing.T) {
	database := setupTestDB(t)
	store := NewStore(database)

	env := map[string]string{"FOO": "bar", "BAZ": "qux"}
	session := &Session{Name: "envtest", Env: env}
	require.NoError(t, store.Add(session))

	got, err := store.GetByID(session.ID)
	require.NoError(t, err)
	assert.Equal(t, env, got.Env)
}

func TestDelete(t *testing.T) {
	database := setupTestDB(t)
	store := NewStore(database)

	session := &Session{Name: "todelete"}
	require.NoError(t, store.Add(session))

	require.NoError(t, store.Delete(session.ID))

	_, err := store.GetByID(session.ID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestList(t *testing.T) {
	database := setupTestDB(t)
	store := NewStore(database)

	require.NoError(t, store.Add(&Session{Name: "one"}))
	require.NoError(t, store.Add(&Session{Name: "two"}))
	require.NoError(t, store.Add(&Session{Name: "three"}))

	list := store.List()
	assert.Len(t, list, 3)
}
