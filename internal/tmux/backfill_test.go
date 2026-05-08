package tmux

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackfillStatus_SetsInactiveOnFreshTableForEmptyStatus(t *testing.T) {
	database := testdb.New(t, &TmuxSession{})
	store := NewTmuxStore(database)

	require.NoError(t, store.Add(&TmuxSession{Name: "fresh"}))

	require.NoError(t, store.BackfillStatus())

	got, err := store.GetByName("fresh")
	require.NoError(t, err)
	assert.Equal(t, TmuxStatusInactive, got.Status)
}

func TestBackfillStatus_PreservesExplicitStatus(t *testing.T) {
	database := testdb.New(t, &TmuxSession{})
	store := NewTmuxStore(database)

	require.NoError(t, store.Add(&TmuxSession{Name: "alive", Status: TmuxStatusActive}))

	require.NoError(t, store.BackfillStatus())

	got, err := store.GetByName("alive")
	require.NoError(t, err)
	assert.Equal(t, TmuxStatusActive, got.Status)
}
