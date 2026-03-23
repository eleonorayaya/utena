package claude

import (
	"context"
	"testing"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/stretchr/testify/require"
)

func setupService(t *testing.T) (*ClaudeService, *ClaudeStore) {
	t.Helper()
	database := setupTestDB(t)
	store := NewClaudeStore(database)
	bus := eventbus.NewEventBus()
	service := NewClaudeService(store, bus)
	service.OnAppStart(context.Background())
	return service, store
}

func TestPreToolUse_ClearsNeedsAttention(t *testing.T) {
	service, store := setupService(t)
	store.Upsert(&ClaudeSession{
		ClaudeSessionID: "cs-1",
		SessionID:       "sess-1",
		Status:          StatusNeedsAttention,
	})

	err := service.HandleHookEvent(context.Background(), &HookEventRequest{
		Event:           "PreToolUse",
		ClaudeSessionID: "cs-1",
		SessionID:       "sess-1",
	})
	require.NoError(t, err)

	sessions := store.ListBySessionID("sess-1")
	require.Len(t, sessions, 1)
	require.Equal(t, StatusWorking, sessions[0].Status)
}

func TestPreToolUse_NoOpWhenWorking(t *testing.T) {
	service, store := setupService(t)
	store.Upsert(&ClaudeSession{
		ClaudeSessionID: "cs-1",
		SessionID:       "sess-1",
		Status:          StatusWorking,
	})

	err := service.HandleHookEvent(context.Background(), &HookEventRequest{
		Event:           "PreToolUse",
		ClaudeSessionID: "cs-1",
		SessionID:       "sess-1",
	})
	require.NoError(t, err)

	sessions := store.ListBySessionID("sess-1")
	require.Len(t, sessions, 1)
	require.Equal(t, StatusWorking, sessions[0].Status)
}

func TestPreToolUse_ClearsReadyForReview(t *testing.T) {
	service, store := setupService(t)
	store.Upsert(&ClaudeSession{
		ClaudeSessionID: "cs-1",
		SessionID:       "sess-1",
		Status:          StatusReadyForReview,
	})

	err := service.HandleHookEvent(context.Background(), &HookEventRequest{
		Event:           "PreToolUse",
		ClaudeSessionID: "cs-1",
		SessionID:       "sess-1",
	})
	require.NoError(t, err)

	sessions := store.ListBySessionID("sess-1")
	require.Len(t, sessions, 1)
	require.Equal(t, StatusWorking, sessions[0].Status)
}

func TestPreToolUse_ClearsCompleted(t *testing.T) {
	service, store := setupService(t)
	store.Upsert(&ClaudeSession{
		ClaudeSessionID: "cs-1",
		SessionID:       "sess-1",
		Status:          StatusCompleted,
	})

	err := service.HandleHookEvent(context.Background(), &HookEventRequest{
		Event:           "PreToolUse",
		ClaudeSessionID: "cs-1",
		SessionID:       "sess-1",
	})
	require.NoError(t, err)

	sessions := store.ListBySessionID("sess-1")
	require.Len(t, sessions, 1)
	require.Equal(t, StatusWorking, sessions[0].Status)
}

func TestFullFlow_NeedsAttentionToWorkingViaPreToolUse(t *testing.T) {
	service, store := setupService(t)
	ctx := context.Background()

	service.HandleHookEvent(ctx, &HookEventRequest{
		Event:           "SessionStart",
		ClaudeSessionID: "cs-1",
		SessionID:       "sess-1",
		CWD:             "/tmp",
	})
	sessions := store.ListBySessionID("sess-1")
	require.Equal(t, StatusWorking, sessions[0].Status)

	service.HandleHookEvent(ctx, &HookEventRequest{
		Event:            "Notification",
		ClaudeSessionID:  "cs-1",
		SessionID:        "sess-1",
		NotificationType: "permission_prompt",
	})
	sessions = store.ListBySessionID("sess-1")
	require.Equal(t, StatusNeedsAttention, sessions[0].Status)

	service.HandleHookEvent(ctx, &HookEventRequest{
		Event:           "PreToolUse",
		ClaudeSessionID: "cs-1",
		SessionID:       "sess-1",
	})
	sessions = store.ListBySessionID("sess-1")
	require.Equal(t, StatusWorking, sessions[0].Status)

	service.HandleHookEvent(ctx, &HookEventRequest{
		Event:           "Stop",
		ClaudeSessionID: "cs-1",
		SessionID:       "sess-1",
	})
	sessions = store.ListBySessionID("sess-1")
	require.Equal(t, StatusReadyForReview, sessions[0].Status)
}

func TestTmuxClientSessionChanged_ClearsReadyForReview(t *testing.T) {
	service, store := setupService(t)
	store.Upsert(&ClaudeSession{
		ClaudeSessionID: "cs-1",
		SessionID:       "sess-1",
		Status:          StatusReadyForReview,
	})

	err := service.eventBus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.TmuxClientSessionChanged,
		Data: eventbus.TmuxHookEvent{TmuxSessionName: "sess-1"},
	})
	require.NoError(t, err)

	sessions := store.ListBySessionID("sess-1")
	require.Len(t, sessions, 1)
	require.Equal(t, StatusCompleted, sessions[0].Status)
}

func TestTmuxClientSessionChanged_DoesNotAffectWorking(t *testing.T) {
	service, store := setupService(t)
	store.Upsert(&ClaudeSession{
		ClaudeSessionID: "cs-1",
		SessionID:       "sess-1",
		Status:          StatusWorking,
	})

	err := service.eventBus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.TmuxClientSessionChanged,
		Data: eventbus.TmuxHookEvent{TmuxSessionName: "sess-1"},
	})
	require.NoError(t, err)

	sessions := store.ListBySessionID("sess-1")
	require.Len(t, sessions, 1)
	require.Equal(t, StatusWorking, sessions[0].Status)
}

func TestTmuxClientSessionChanged_DoesNotAffectNeedsAttention(t *testing.T) {
	service, store := setupService(t)
	store.Upsert(&ClaudeSession{
		ClaudeSessionID: "cs-1",
		SessionID:       "sess-1",
		Status:          StatusNeedsAttention,
	})

	err := service.eventBus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.TmuxClientSessionChanged,
		Data: eventbus.TmuxHookEvent{TmuxSessionName: "sess-1"},
	})
	require.NoError(t, err)

	sessions := store.ListBySessionID("sess-1")
	require.Len(t, sessions, 1)
	require.Equal(t, StatusNeedsAttention, sessions[0].Status)
}
