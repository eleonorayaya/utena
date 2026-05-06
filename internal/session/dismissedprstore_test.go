package session

import (
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/stretchr/testify/require"
)

func setupDismissedPRStore(t *testing.T) *DismissedPRStore {
	t.Helper()
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	require.NoError(t, database.Migrate(&DismissedPR{}))
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Logf("close database: %v", err)
		}
	})
	return NewDismissedPRStore(database)
}

func TestAddAndIsDismissed(t *testing.T) {
	store := setupDismissedPRStore(t)

	err := store.Add(&DismissedPR{PullRequestID: 42, DismissedAt: time.Now()})
	require.NoError(t, err)

	require.True(t, store.IsDismissed(42))
}

func TestIsDismissed_Unknown(t *testing.T) {
	store := setupDismissedPRStore(t)

	require.False(t, store.IsDismissed(999))
}

func TestAdd_Uniqueness(t *testing.T) {
	store := setupDismissedPRStore(t)

	err := store.Add(&DismissedPR{PullRequestID: 42, DismissedAt: time.Now()})
	require.NoError(t, err)

	err = store.Add(&DismissedPR{PullRequestID: 42, DismissedAt: time.Now()})
	require.Error(t, err)
}

func TestDeleteThenIsDismissed(t *testing.T) {
	store := setupDismissedPRStore(t)

	err := store.Add(&DismissedPR{PullRequestID: 42, DismissedAt: time.Now()})
	require.NoError(t, err)
	require.True(t, store.IsDismissed(42))

	err = store.Delete(42)
	require.NoError(t, err)
	require.False(t, store.IsDismissed(42))
}
