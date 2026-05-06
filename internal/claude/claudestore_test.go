package claude

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type testSession struct {
	gorm.Model
}

func (testSession) TableName() string { return "sessions" }

func setupTestDB(t *testing.T) db.Database {
	return testdb.New(t, &testSession{}, &ClaudeSession{})
}

func createTestSession(t *testing.T, database db.Database) uint {
	t.Helper()
	sess := &testSession{}
	database.Create(sess)
	return sess.ID
}

func setupStore(t *testing.T) (*ClaudeStore, db.Database) {
	t.Helper()
	database := setupTestDB(t)
	return NewClaudeStore(database), database
}

func TestUpdateStatus(t *testing.T) {
	store, database := setupStore(t)
	sessionID := createTestSession(t, database)

	require.NoError(t, store.Create(&ClaudeSession{
		ClaudeSessionID: "cs-1",
		SessionID:       sessionID,
		Status:          StatusNeedsAttention,
	}))

	err := store.UpdateStatus("cs-1", StatusWorking)
	require.NoError(t, err)

	sessions := store.List()
	require.Len(t, sessions, 1)
	require.Equal(t, StatusWorking, sessions[0].Status)
}

func TestUpdateStatus_NonExistentSession(t *testing.T) {
	store, _ := setupStore(t)

	err := store.UpdateStatus("nonexistent", StatusWorking)
	require.NoError(t, err)
}
