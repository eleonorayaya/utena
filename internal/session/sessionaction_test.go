package session

import (
	"errors"
	"testing"

	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/require"
)

type fakeSpawner struct {
	calls []spawnCall
	err   error
}

type spawnCall struct {
	sessionName string
	startDir    string
	command     string
}

func (f *fakeSpawner) SpawnWindow(sessionName, startDir, command string) error {
	f.calls = append(f.calls, spawnCall{sessionName, startDir, command})
	return f.err
}

func setupActionTestDB(t *testing.T) (*SessionActionStore, *Session) {
	t.Helper()
	database := setupTestDB(t)
	store := NewSessionActionStore(database)
	ws := &workspace.Workspace{Name: "test-ws", Path: "/tmp/test"}
	require.NoError(t, database.Create(ws).Error)
	sess := &Session{Name: "test", WorkspaceID: ws.ID, Status: StatusPending}
	require.NoError(t, database.Create(sess).Error)
	return store, sess
}

func TestSessionActionStore_AddAndList(t *testing.T) {
	store, sess := setupActionTestDB(t)

	action := &SessionAction{
		SessionID: sess.ID,
		Trigger:   TriggerOnCreate,
		Type:      SessionActionTypeClaude,
		Options:   marshalOptions(ClaudeActionOptions{Prompt: "/review"}),
	}
	require.NoError(t, store.Add(action))
	require.NotZero(t, action.ID)

	actions, err := store.ListBySessionIDAndTrigger(sess.ID, TriggerOnCreate)
	require.NoError(t, err)
	require.Len(t, actions, 1)
	require.Equal(t, SessionActionTypeClaude, actions[0].Type)
	require.Equal(t, TriggerOnCreate, actions[0].Trigger)
	require.Contains(t, actions[0].Options, "/review")

	other, err := store.ListBySessionIDAndTrigger(sess.ID+999, TriggerOnCreate)
	require.NoError(t, err)
	require.Empty(t, other)
}

func TestSessionActionStore_UpdateError(t *testing.T) {
	store, sess := setupActionTestDB(t)

	action := &SessionAction{
		SessionID: sess.ID,
		Trigger:   TriggerOnCreate,
		Type:      SessionActionTypeClaude,
		Options:   marshalOptions(ClaudeActionOptions{Prompt: "/review"}),
	}
	require.NoError(t, store.Add(action))

	require.NoError(t, store.UpdateError(action, "something failed"))
	actions, _ := store.ListBySessionIDAndTrigger(sess.ID, TriggerOnCreate)
	require.Equal(t, "something failed", actions[0].Error)

	require.NoError(t, store.UpdateError(action, ""))
	actions, _ = store.ListBySessionIDAndTrigger(sess.ID, TriggerOnCreate)
	require.Empty(t, actions[0].Error)
}

func TestExecuteSessionActions_Claude_SpawnsWindow(t *testing.T) {
	store, sess := setupActionTestDB(t)
	action := &SessionAction{
		SessionID: sess.ID,
		Trigger:   TriggerOnCreate,
		Type:      SessionActionTypeClaude,
		Options:   marshalOptions(ClaudeActionOptions{Prompt: "/review"}),
	}
	require.NoError(t, store.Add(action))

	spawner := &fakeSpawner{}
	executeSessionActions([]SessionAction{*action}, spawner, store, "my-session", "/workspace")

	require.Len(t, spawner.calls, 1)
	require.Equal(t, "my-session", spawner.calls[0].sessionName)
	require.Equal(t, "/workspace", spawner.calls[0].startDir)
	require.Equal(t, `claude "/review"`, spawner.calls[0].command)

	actions, _ := store.ListBySessionIDAndTrigger(sess.ID, TriggerOnCreate)
	require.Empty(t, actions[0].Error)
}

func TestExecuteSessionActions_SpawnError_PersistsError(t *testing.T) {
	store, sess := setupActionTestDB(t)
	action := &SessionAction{
		SessionID: sess.ID,
		Trigger:   TriggerOnCreate,
		Type:      SessionActionTypeClaude,
		Options:   marshalOptions(ClaudeActionOptions{Prompt: "/review"}),
	}
	require.NoError(t, store.Add(action))

	spawner := &fakeSpawner{err: errors.New("tmux unavailable")}
	executeSessionActions([]SessionAction{*action}, spawner, store, "my-session", "/workspace")

	actions, _ := store.ListBySessionIDAndTrigger(sess.ID, TriggerOnCreate)
	require.Contains(t, actions[0].Error, "tmux unavailable")
}

func TestExecuteSessionActions_UnknownType_Skipped(t *testing.T) {
	store, sess := setupActionTestDB(t)
	action := &SessionAction{
		SessionID: sess.ID,
		Trigger:   TriggerOnCreate,
		Type:      "unknown",
		Options:   "{}",
	}
	require.NoError(t, store.Add(action))

	spawner := &fakeSpawner{}
	executeSessionActions([]SessionAction{*action}, spawner, store, "my-session", "/workspace")

	require.Empty(t, spawner.calls)
	actions, _ := store.ListBySessionIDAndTrigger(sess.ID, TriggerOnCreate)
	require.Empty(t, actions[0].Error)
}

func TestExecuteSessionActions_SpawnError_DoesNotPanic(t *testing.T) {
	store, sess := setupActionTestDB(t)
	action := &SessionAction{
		SessionID: sess.ID,
		Trigger:   TriggerOnCreate,
		Type:      SessionActionTypeClaude,
		Options:   marshalOptions(ClaudeActionOptions{Prompt: "/review"}),
	}
	require.NoError(t, store.Add(action))

	spawner := &fakeSpawner{err: errors.New("tmux unavailable")}
	require.NotPanics(t, func() {
		executeSessionActions([]SessionAction{*action}, spawner, store, "my-session", "/workspace")
	})
}
