package claude

import (
	"context"
	"testing"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/stretchr/testify/require"
)

func setupService(t *testing.T) (*ClaudeService, *ClaudeStore, db.Database) {
	t.Helper()
	database := setupTestDB(t)
	store := NewClaudeStore(database)
	service := NewClaudeService(store)
	return service, store, database
}

func TestPreToolUse_ClearsNeedsAttention(t *testing.T) {
	service, store, database := setupService(t)
	sessionID := createTestSession(t, database)

	store.Create(&ClaudeSession{
		ClaudeSessionID: "cs-1",
		SessionID:       sessionID,
		Status:          StatusNeedsAttention,
	})

	err := service.HandleHookEvent(context.Background(), &HookEventRequest{
		Event:           "PreToolUse",
		ClaudeSessionID: "cs-1",
		SessionID:       sessionID,
	})
	require.NoError(t, err)

	sessions := store.List()
	require.Len(t, sessions, 1)
	require.Equal(t, StatusWorking, sessions[0].Status)
}

func TestPreToolUse_NoOpWhenWorking(t *testing.T) {
	service, store, database := setupService(t)
	sessionID := createTestSession(t, database)

	store.Create(&ClaudeSession{
		ClaudeSessionID: "cs-1",
		SessionID:       sessionID,
		Status:          StatusWorking,
	})

	err := service.HandleHookEvent(context.Background(), &HookEventRequest{
		Event:           "PreToolUse",
		ClaudeSessionID: "cs-1",
		SessionID:       sessionID,
	})
	require.NoError(t, err)

	sessions := store.List()
	require.Len(t, sessions, 1)
	require.Equal(t, StatusWorking, sessions[0].Status)
}

func TestPreToolUse_ClearsReadyForReview(t *testing.T) {
	service, store, database := setupService(t)
	sessionID := createTestSession(t, database)

	store.Create(&ClaudeSession{
		ClaudeSessionID: "cs-1",
		SessionID:       sessionID,
		Status:          StatusReadyForReview,
	})

	err := service.HandleHookEvent(context.Background(), &HookEventRequest{
		Event:           "PreToolUse",
		ClaudeSessionID: "cs-1",
		SessionID:       sessionID,
	})
	require.NoError(t, err)

	sessions := store.List()
	require.Len(t, sessions, 1)
	require.Equal(t, StatusWorking, sessions[0].Status)
}

func TestPreToolUse_ClearsDone(t *testing.T) {
	service, store, database := setupService(t)
	sessionID := createTestSession(t, database)

	store.Create(&ClaudeSession{
		ClaudeSessionID: "cs-1",
		SessionID:       sessionID,
		Status:          StatusDone,
	})

	err := service.HandleHookEvent(context.Background(), &HookEventRequest{
		Event:           "PreToolUse",
		ClaudeSessionID: "cs-1",
		SessionID:       sessionID,
	})
	require.NoError(t, err)

	sessions := store.List()
	require.Len(t, sessions, 1)
	require.Equal(t, StatusWorking, sessions[0].Status)
}

func TestFullFlow_NeedsAttentionToWorkingViaPreToolUse(t *testing.T) {
	service, store, database := setupService(t)
	sessionID := createTestSession(t, database)
	ctx := context.Background()

	service.HandleHookEvent(ctx, &HookEventRequest{
		Event:           "SessionStart",
		ClaudeSessionID: "cs-1",
		SessionID:       sessionID,
		CWD:             "/tmp",
	})
	sessions := store.List()
	require.Equal(t, StatusIdle, sessions[0].Status)

	service.HandleHookEvent(ctx, &HookEventRequest{
		Event:            "Notification",
		ClaudeSessionID:  "cs-1",
		SessionID:        sessionID,
		NotificationType: "permission_prompt",
	})
	sessions = store.List()
	require.Equal(t, StatusNeedsAttention, sessions[0].Status)

	service.HandleHookEvent(ctx, &HookEventRequest{
		Event:           "PreToolUse",
		ClaudeSessionID: "cs-1",
		SessionID:       sessionID,
	})
	sessions = store.List()
	require.Equal(t, StatusWorking, sessions[0].Status)

	service.HandleHookEvent(ctx, &HookEventRequest{
		Event:           "Stop",
		ClaudeSessionID: "cs-1",
		SessionID:       sessionID,
	})
	sessions = store.List()
	require.Equal(t, StatusReadyForReview, sessions[0].Status)
}
