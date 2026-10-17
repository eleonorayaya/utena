package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func testMcpCatalog() (bundles.Catalog, bundles.Servers) {
	catalog := testBundleCatalog()
	catalog["homelab"] = bundles.Bundle{McpServers: []string{"ha"}}
	catalog["broken"] = bundles.Bundle{McpServers: []string{"ghost"}}
	return catalog, bundles.Servers{"ha": json.RawMessage(`{"type":"http","url":"https://x"}`)}
}

func readMcpServers(t *testing.T, root string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".mcp.json"))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	servers, _ := raw["mcpServers"].(map[string]any)
	return servers
}

func readEnabledMcpServers(t *testing.T, root string) []any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude", "settings.local.json"))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	approved, _ := raw["enabledMcpjsonServers"].([]any)
	return approved
}

func addMcpTestSession(t *testing.T) (*SessionService, *Session) {
	t.Helper()
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)
	service.SetBundleCatalog(testMcpCatalog())
	swStore := NewSessionWorktreeStore(sessionStore.db)
	sess := &Session{
		Name:        "mcp",
		Status:      StatusActive,
		LastUsedAt:  time.Now(),
		SessionRoot: t.TempDir(),
	}
	addTestSession(t, sessionStore, swStore, sess, ws1ID)
	return service, sess
}

func TestSessionService_SetBundles_WritesMcpServers(t *testing.T) {
	service, sess := addMcpTestSession(t)
	ctx := context.Background()

	_, err := service.SetBundles(ctx, sess.ID, []string{"homelab"})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"ha": map[string]any{"type": "http", "url": "https://x"}}, readMcpServers(t, sess.SessionRoot))
	require.Equal(t, []any{"ha"}, readEnabledMcpServers(t, sess.SessionRoot))

	_, err = service.SetBundles(ctx, sess.ID, []string{})
	require.NoError(t, err)
	require.Empty(t, readMcpServers(t, sess.SessionRoot))
	require.Empty(t, readEnabledMcpServers(t, sess.SessionRoot))
}

func TestSessionService_SetBundles_SkipsUndefinedMcpServers(t *testing.T) {
	service, sess := addMcpTestSession(t)

	_, err := service.SetBundles(context.Background(), sess.ID, []string{"broken"})
	require.NoError(t, err)
	require.NotContains(t, readMcpServers(t, sess.SessionRoot), "ghost")
	require.NotContains(t, readEnabledMcpServers(t, sess.SessionRoot), "ghost")
}

func TestSessionService_CreateSession_DefaultBundlesWriteMcpServers(t *testing.T) {
	service, sessionStore, _, ws1ID, _ := setupTwoRepoSessionService(t)
	service.SetBundleCatalog(testMcpCatalog())
	writeWorkspaceConfig(t, workspacePath(t, service, ws1ID), `{"default_bundles":["homelab"]}`)

	sess, err := service.CreateSession(context.Background(), CreateSessionInput{
		Name:       "homelab",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: ws1ID, BaseBranch: "main"}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"homelab"}, sess.Bundles)

	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)
	require.Equal(t, map[string]any{"ha": map[string]any{"type": "http", "url": "https://x"}}, readMcpServers(t, sess.SessionRoot))
	require.Equal(t, []any{"ha"}, readEnabledMcpServers(t, sess.SessionRoot))
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
	service.SetBundleCatalog(testBundleCatalog(), nil)
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
	service, sessionStore, _, ws1ID, _ := setupTwoRepoSessionService(t)
	service.SetBundleCatalog(testBundleCatalog(), nil)
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

	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)
	waitForStatus(t, sessionStore, none.ID, StatusActive, 5*time.Second)
	require.Equal(t, map[string]any{"docs@m": true}, readEnabledPlugins(t, sess.SessionRoot))
	require.Empty(t, readEnabledPlugins(t, none.SessionRoot))
}

func TestSessionService_ApplyClaudeSettings_UsesStoredBundles(t *testing.T) {
	service, sessionStore, _, ws1ID, _ := setupTwoRepoSessionService(t)
	service.SetBundleCatalog(testBundleCatalog(), nil)

	ctx := context.Background()
	sess, err := service.CreateSession(ctx, CreateSessionInput{
		Name:       "stale",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: ws1ID, BaseBranch: "main"}},
		Bundles:    []string{"go"},
	})
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	require.NoError(t, sessionStore.UpdateBundles(sess.ID, []string{"docs"}))
	require.Equal(t, []string{"go"}, sess.Bundles)

	require.Empty(t, service.applyClaudeSettings(ctx, sess))
	require.Equal(t, []string{"docs"}, sess.Bundles)
	require.Equal(t, map[string]any{"docs@m": true}, readEnabledPlugins(t, sess.SessionRoot))
}

func TestSessionService_WriteBundles_SkipsEmptySessionRoot(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)
	service.SetBundleCatalog(testBundleCatalog(), nil)
	swStore := NewSessionWorktreeStore(sessionStore.db)

	sess := &Session{
		Name:       "legacy",
		Status:     StatusActive,
		LastUsedAt: time.Now(),
		Bundles:    []string{"go"},
	}
	addTestSession(t, sessionStore, swStore, sess, ws1ID)

	cwd := t.TempDir()
	t.Chdir(cwd)

	require.NoError(t, service.writeBundles(context.Background(), sess.SessionRoot, sess.Bundles))
	_, err := os.Stat(filepath.Join(cwd, ".claude"))
	require.True(t, os.IsNotExist(err))
}

func TestSessionService_AddWorkspace_UnionsDefaultBundles(t *testing.T) {
	service, sessionStore, _, ws1ID, ws2ID := setupTwoRepoSessionService(t)
	service.SetBundleCatalog(testBundleCatalog(), nil)
	writeWorkspaceConfig(t, workspacePath(t, service, ws1ID), `{"default_bundles":["go"]}`)
	writeWorkspaceConfig(t, workspacePath(t, service, ws2ID), `{"default_bundles":["docs"]}`)

	ctx := context.Background()
	sess, err := service.CreateSession(ctx, CreateSessionInput{
		Name:       "grow",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: ws1ID, BaseBranch: "main"}},
	})
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	updated, err := service.AddWorkspace(ctx, sess.ID, WorkspaceBranchSpec{WorkspaceID: ws2ID, BaseBranch: "main"})
	require.NoError(t, err)
	require.Equal(t, []string{"go", "docs"}, updated.Bundles)

	waitForUpdatedAfter(t, sessionStore, sess.ID, updated.UpdatedAt, 10*time.Second)
	require.Equal(t, true, readEnabledPlugins(t, sess.SessionRoot)["docs@m"])
}

func TestSessionService_ActivateSession_ReappliesPlugins(t *testing.T) {
	service, sessionStore, _, ws1ID, _ := setupTwoRepoSessionService(t)
	service.SetBundleCatalog(testBundleCatalog(), nil)

	ctx := context.Background()
	sess, err := service.CreateSession(ctx, CreateSessionInput{
		Name:       "revive",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: ws1ID, BaseBranch: "main"}},
		Bundles:    []string{"go"},
	})
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	_, err = service.ArchiveSession(ctx, sess.ID)
	require.NoError(t, err)

	service.SetBundleCatalog(bundles.Catalog{"go": {Plugins: []string{"go@m", "gopls@m"}}}, nil)
	_, err = service.ActivateSession(ctx, sess.ID)
	require.NoError(t, err)

	require.Equal(t, map[string]any{"go@m": true, "gopls@m": true}, readEnabledPlugins(t, sess.SessionRoot))
}

func TestSessionService_SetBundles_MalformedMcpJSONStillWritesPlugins(t *testing.T) {
	service, sess := addMcpTestSession(t)
	require.NoError(t, os.WriteFile(filepath.Join(sess.SessionRoot, ".mcp.json"), []byte("{not json"), 0o644))

	_, err := service.SetBundles(context.Background(), sess.ID, []string{"go", "homelab"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse .mcp.json")
	require.NotContains(t, err.Error(), "settings.local.json")
	require.Equal(t, map[string]any{"go@m": true}, readEnabledPlugins(t, sess.SessionRoot))
}

func TestSessionService_ApplyClaudeSettings_MalformedMcpJSONWarnsOnce(t *testing.T) {
	service, sess := addMcpTestSession(t)
	require.NoError(t, service.store.UpdateBundles(sess.ID, []string{"go", "homelab"}))
	require.NoError(t, os.WriteFile(filepath.Join(sess.SessionRoot, ".mcp.json"), []byte("{not json"), 0o644))

	warnings := service.applyClaudeSettings(context.Background(), sess)
	require.Len(t, warnings, 1)
	require.True(t, strings.HasPrefix(warnings[0], "claude-bundles: "), warnings[0])
	require.NotContains(t, warnings[0], "\n")
	require.Equal(t, map[string]any{"go@m": true}, readEnabledPlugins(t, sess.SessionRoot))
}

func TestSessionService_ApplyClaudeSettings_MalformedFilesWarnOnceOnOneLine(t *testing.T) {
	service, sess := addMcpTestSession(t)
	require.NoError(t, service.store.UpdateBundles(sess.ID, []string{"go", "homelab"}))
	require.NoError(t, os.WriteFile(filepath.Join(sess.SessionRoot, ".mcp.json"), []byte("{not json"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(sess.SessionRoot, ".claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sess.SessionRoot, ".claude", "settings.local.json"), []byte("{not json"), 0o644))

	warnings := service.applyClaudeSettings(context.Background(), sess)
	var bundleWarnings []string
	for _, w := range warnings {
		require.NotContains(t, w, "\n")
		require.False(t, strings.HasPrefix(w, "claude-plugins: ") || strings.HasPrefix(w, "claude-mcp: "), w)
		if strings.HasPrefix(w, "claude-bundles: ") {
			bundleWarnings = append(bundleWarnings, w)
		}
	}
	require.Len(t, bundleWarnings, 1, warnings)
	require.Contains(t, bundleWarnings[0], ".mcp.json")
	require.Contains(t, bundleWarnings[0], "settings.local.json")
}

func TestSessionService_ActivateSession_WritesMcpServers(t *testing.T) {
	service, sessionStore, _, ws1ID, _ := setupTwoRepoSessionService(t)
	catalog, servers := testMcpCatalog()
	service.SetBundleCatalog(catalog, servers)

	ctx := context.Background()
	sess, err := service.CreateSession(ctx, CreateSessionInput{
		Name:       "revive-mcp",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: ws1ID, BaseBranch: "main"}},
		Bundles:    []string{"homelab"},
	})
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	_, err = service.ArchiveSession(ctx, sess.ID)
	require.NoError(t, err)

	service.SetBundleCatalog(catalog, bundles.Servers{"ha": json.RawMessage(`{"type":"http","url":"https://y"}`)})
	_, err = service.ActivateSession(ctx, sess.ID)
	require.NoError(t, err)

	require.Equal(t, map[string]any{"ha": map[string]any{"type": "http", "url": "https://y"}}, readMcpServers(t, sess.SessionRoot))
	require.Equal(t, []any{"ha"}, readEnabledMcpServers(t, sess.SessionRoot))
}
