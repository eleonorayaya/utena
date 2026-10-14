package mux

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackfillStatus_SetsInactiveOnFreshTableForEmptyStatus(t *testing.T) {
	database := testdb.New(t, &Session{})
	store := NewStore(database)

	require.NoError(t, store.Add(&Session{Name: "fresh"}))

	require.NoError(t, store.BackfillStatus())

	got, err := store.GetByName("fresh")
	require.NoError(t, err)
	assert.Equal(t, StatusInactive, got.Status)
}

func TestBackfillStatus_PreservesExplicitStatus(t *testing.T) {
	database := testdb.New(t, &Session{})
	store := NewStore(database)

	require.NoError(t, store.Add(&Session{Name: "alive", Status: StatusActive}))

	require.NoError(t, store.BackfillStatus())

	got, err := store.GetByName("alive")
	require.NoError(t, err)
	assert.Equal(t, StatusActive, got.Status)
}
