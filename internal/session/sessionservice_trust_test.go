package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func readTrust(t *testing.T, configPath, root string) any {
	t.Helper()
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	projects, _ := raw["projects"].(map[string]any)
	entry, _ := projects[root].(map[string]any)
	return entry["hasTrustDialogAccepted"]
}

func TestSessionService_CreateSession_TrustsSessionRoot(t *testing.T) {
	service, sessionStore, _, ws1ID, _ := setupTwoRepoSessionService(t)
	configPath := filepath.Join(t.TempDir(), ".claude.json")
	service.SetClaudeGlobalConfigPath(configPath)

	sess, err := service.CreateSession(context.Background(), CreateSessionInput{
		Name:       "trusted",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: ws1ID, BaseBranch: "main"}},
	})
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	require.NotEmpty(t, sess.SessionRoot)
	require.Equal(t, true, readTrust(t, configPath, sess.SessionRoot))
}

func TestSessionService_ActivateSession_TrustsOncePerDaemon(t *testing.T) {
	service, sessionStore, _, ws1ID, _ := setupTwoRepoSessionService(t)

	ctx := context.Background()
	sess, err := service.CreateSession(ctx, CreateSessionInput{
		Name:       "retrust",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: ws1ID, BaseBranch: "main"}},
	})
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	configPath := filepath.Join(t.TempDir(), ".claude.json")
	service.SetClaudeGlobalConfigPath(configPath)

	_, err = service.ActivateSession(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, true, readTrust(t, configPath, sess.SessionRoot))

	require.NoError(t, os.WriteFile(configPath, []byte(`{"projects":{}}`), 0o600))
	_, err = service.ArchiveSession(ctx, sess.ID)
	require.NoError(t, err)
	_, err = service.ActivateSession(ctx, sess.ID)
	require.NoError(t, err)
	require.Nil(t, readTrust(t, configPath, sess.SessionRoot))
}

func TestSessionService_TrustDisabledWithoutConfigPath(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	t.Setenv("HOME", configDir)
	service, sessionStore, _, ws1ID, _ := setupTwoRepoSessionService(t)

	ctx := context.Background()
	sess, err := service.CreateSession(ctx, CreateSessionInput{
		Name:       "untrusted",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: ws1ID, BaseBranch: "main"}},
	})
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)
	_, err = service.ArchiveSession(ctx, sess.ID)
	require.NoError(t, err)
	_, err = service.ActivateSession(ctx, sess.ID)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(configDir, ".claude.json"))
	require.True(t, os.IsNotExist(err), "unexpected %v", err)
}
