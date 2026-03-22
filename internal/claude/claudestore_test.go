package claude

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) db.Database {
	t.Helper()
	database, err := db.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	database.Migrate(&ClaudeSession{})
	t.Cleanup(func() { database.Close() })
	return database
}

func setupStore(t *testing.T) *ClaudeStore {
	t.Helper()
	return NewClaudeStore(setupTestDB(t))
}

func TestUpdateStatusByClaudeSessionID_MatchingFromStatus(t *testing.T) {
	store := setupStore(t)
	store.Upsert(&ClaudeSession{
		ClaudeSessionID: "cs-1",
		SessionID:       "sess-1",
		Status:          StatusNeedsAttention,
	})

	err := store.UpdateStatusByClaudeSessionID("cs-1", StatusNeedsAttention, StatusWorking)
	require.NoError(t, err)

	sessions := store.ListBySessionID("sess-1")
	require.Len(t, sessions, 1)
	require.Equal(t, StatusWorking, sessions[0].Status)
}

func TestUpdateStatusByClaudeSessionID_NonMatchingFromStatus(t *testing.T) {
	store := setupStore(t)
	store.Upsert(&ClaudeSession{
		ClaudeSessionID: "cs-1",
		SessionID:       "sess-1",
		Status:          StatusWorking,
	})

	err := store.UpdateStatusByClaudeSessionID("cs-1", StatusNeedsAttention, StatusWorking)
	require.NoError(t, err)

	sessions := store.ListBySessionID("sess-1")
	require.Len(t, sessions, 1)
	require.Equal(t, StatusWorking, sessions[0].Status)
}

func TestUpdateStatusByClaudeSessionID_NonExistentSession(t *testing.T) {
	store := setupStore(t)

	err := store.UpdateStatusByClaudeSessionID("nonexistent", StatusNeedsAttention, StatusWorking)
	require.NoError(t, err)
}
