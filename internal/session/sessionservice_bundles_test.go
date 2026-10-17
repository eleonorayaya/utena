package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/bundles"
	"github.com/stretchr/testify/require"
)

func testBundleCatalog() bundles.Catalog {
	return bundles.Catalog{
		"go":   {Plugins: []string{"go@m"}},
		"docs": {Plugins: []string{"docs@m"}},
	}
}

func workspacePath(t *testing.T, service *SessionService, wsID uint) string {
	t.Helper()
	ws, err := service.workspaceService.GetWorkspace(context.Background(), wsID)
	require.NoError(t, err)
	return ws.Path
}

func readEnabledPlugins(t *testing.T, root string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude", "settings.local.json"))
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	plugins, _ := raw["enabledPlugins"].(map[string]any)
	return plugins
}

func TestSessionService_CreateSession_DefaultsBundlesFromWorkspaces(t *testing.T) {
	service, sessionStore, _, ws1ID, ws2ID := setupTwoRepoSessionService(t)
	service.SetBundleCatalog(testBundleCatalog())
	writeWorkspaceConfig(t, workspacePath(t, service, ws1ID), `{"default_bundles":["go"]}`)
	writeWorkspaceConfig(t, workspacePath(t, service, ws2ID), `{"default_bundles":["go","docs"]}`)

	sess, err := service.CreateSession(context.Background(), CreateSessionInput{
		Name: "bundled",
		Workspaces: []WorkspaceBranchSpec{
			{WorkspaceID: ws1ID, BaseBranch: "main"},
			{WorkspaceID: ws2ID, BaseBranch: "main"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"go", "docs"}, sess.Bundles)

	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)
	require.Equal(t, map[string]any{"go@m": true, "docs@m": true}, readEnabledPlugins(t, sess.SessionRoot))

	stored, err := sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"go", "docs"}, stored.Bundles)
}

func TestSessionService_CreateSession_ExplicitBundlesOverrideDefaults(t *testing.T) {
	service, _, _, ws1ID, _ := setupTwoRepoSessionService(t)
	service.SetBundleCatalog(testBundleCatalog())
	writeWorkspaceConfig(t, workspacePath(t, service, ws1ID), `{"default_bundles":["go"]}`)

	sess, err := service.CreateSession(context.Background(), CreateSessionInput{
		Name:       "explicit",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: ws1ID, BaseBranch: "main"}},
		Bundles:    []string{"docs"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"docs"}, sess.Bundles)

	none, err := service.CreateSession(context.Background(), CreateSessionInput{
		Name:       "none",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: ws1ID, BaseBranch: "main"}},
		Bundles:    []string{},
	})
	require.NoError(t, err)
	require.Empty(t, none.Bundles)
}
