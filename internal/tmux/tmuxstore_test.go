package tmux

import (
	"errors"
	"testing"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) db.Database {
	t.Helper()
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	require.NoError(t, database.Migrate(&TmuxSession{}))
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Logf("close database: %v", err)
		}
	})
	return database
}

func TestAddAndGetByID(t *testing.T) {
	database := setupTestDB(t)
	store := NewTmuxStore(database)

	session := &TmuxSession{
		Name:     "dev",
		StartDir: "/home/user/dev",
		Env:      map[string]string{"TERM": "xterm"},
		IsAlive:  true,
	}
	require.NoError(t, store.Add(session))
	assert.NotZero(t, session.ID)

	got, err := store.GetByID(session.ID)
	require.NoError(t, err)
	assert.Equal(t, "dev", got.Name)
	assert.Equal(t, "/home/user/dev", got.StartDir)
	assert.True(t, got.IsAlive)
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
}

func TestUpdateIsAlive(t *testing.T) {
	database := setupTestDB(t)
	store := NewTmuxStore(database)

	session := &TmuxSession{Name: "sess", IsAlive: false}
	require.NoError(t, store.Add(session))

	session.IsAlive = true
	require.NoError(t, store.Update(session))

	got, err := store.GetByID(session.ID)
	require.NoError(t, err)
	assert.True(t, got.IsAlive)
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
