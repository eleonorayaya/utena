package session

import (
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/require"
)

func setupDismissedPRStore(t *testing.T) *DismissedPRStore {
	return NewDismissedPRStore(testdb.New(t, &DismissedPR{}))
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
