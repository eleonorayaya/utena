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

func TestStartupActionStore_AddAndList(t *testing.T) {
	database := setupTestDB(t)
	store := NewStartupActionStore(database)

	ws := &workspace.Workspace{Name: "test-ws", Path: "/tmp/test"}
	require.NoError(t, database.Create(ws).Error)

	sess := &Session{Name: "test", WorkspaceID: ws.ID, Status: StatusPending}
	require.NoError(t, database.Create(sess).Error)

	action := &StartupAction{
		SessionID: sess.ID,
		Type:      StartupActionTypeClaude,
		Options:   marshalOptions(ClaudeActionOptions{Prompt: "/review"}),
	}
	require.NoError(t, store.Add(action))
	require.NotZero(t, action.ID)

	actions, err := store.ListBySessionID(sess.ID)
	require.NoError(t, err)
	require.Len(t, actions, 1)
	require.Equal(t, StartupActionTypeClaude, actions[0].Type)
	require.Contains(t, actions[0].Options, "/review")

	other, err := store.ListBySessionID(sess.ID + 999)
	require.NoError(t, err)
	require.Empty(t, other)
}

func TestExecuteStartupActions_Claude_SpawnsWindow(t *testing.T) {
	spawner := &fakeSpawner{}
	actions := []StartupAction{
		{
			Type:    StartupActionTypeClaude,
			Options: marshalOptions(ClaudeActionOptions{Prompt: "/review"}),
		},
	}

	executeStartupActions(actions, spawner, "my-session", "/workspace")

	require.Len(t, spawner.calls, 1)
	require.Equal(t, "my-session", spawner.calls[0].sessionName)
	require.Equal(t, "/workspace", spawner.calls[0].startDir)
	require.Equal(t, `claude "/review"`, spawner.calls[0].command)
}

func TestExecuteStartupActions_UnknownType_Skipped(t *testing.T) {
	spawner := &fakeSpawner{}
	actions := []StartupAction{
		{Type: "unknown", Options: "{}"},
	}

	executeStartupActions(actions, spawner, "my-session", "/workspace")

	require.Empty(t, spawner.calls)
}

func TestExecuteStartupActions_SpawnError_DoesNotPanic(t *testing.T) {
	spawner := &fakeSpawner{err: errors.New("tmux unavailable")}
	actions := []StartupAction{
		{
			Type:    StartupActionTypeClaude,
			Options: marshalOptions(ClaudeActionOptions{Prompt: "/review"}),
		},
	}

	require.NotPanics(t, func() {
		executeStartupActions(actions, spawner, "my-session", "/workspace")
	})
}
