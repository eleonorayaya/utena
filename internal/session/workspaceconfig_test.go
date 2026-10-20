package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/require"
)

func loadLocalWorkspaceConfig(wsPath string) (*WorkspaceConfig, error) {
	return loadWorkspaceConfig(context.Background(), workspace.NewLocalSourceReader(""), workspace.Workspace{Path: wsPath})
}

func localDefaultBundles(t *testing.T, paths ...string) []string {
	t.Helper()
	wss := make([]workspace.Workspace, len(paths))
	for i, p := range paths {
		wss[i] = workspace.Workspace{Path: p}
	}
	out, err := defaultBundles(context.Background(), workspace.NewLocalSourceReader(""), wss)
	require.NoError(t, err)
	return out
}

func writeWorkspaceConfig(t *testing.T, wsPath, contents string) {
	t.Helper()
	dir := filepath.Join(wsPath, ".utena")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte(contents), 0o644))
}

func TestLoadWorkspaceConfig_Valid(t *testing.T) {
	wsPath := t.TempDir()
	writeWorkspaceConfig(t, wsPath, `{
		"setup_actions": [
			{"type": "copy_file", "src": ".env.example", "dest": ".env", "dest_relative_to": "workspace_dir"},
			{"type": "install_deps", "package_manager": "auto"}
		]
	}`)

	cfg, err := loadLocalWorkspaceConfig(wsPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, cfg.SetupActions, 2)

	require.Equal(t, WorkspaceActionCopyFile, cfg.SetupActions[0].Type)
	require.Equal(t, ".env.example", cfg.SetupActions[0].Src)
	require.Equal(t, ".env", cfg.SetupActions[0].Dest)
	require.Equal(t, DestRelativeToWorkspaceDir, cfg.SetupActions[0].DestRelativeTo)

	require.Equal(t, WorkspaceActionInstallDeps, cfg.SetupActions[1].Type)
	require.Equal(t, packageManagerAuto, cfg.SetupActions[1].PackageManager)
}

func TestLoadWorkspaceConfig_Missing(t *testing.T) {
	cfg, err := loadLocalWorkspaceConfig(t.TempDir())
	require.NoError(t, err)
	require.Nil(t, cfg)
}

func TestLoadWorkspaceConfig_Malformed(t *testing.T) {
	wsPath := t.TempDir()
	writeWorkspaceConfig(t, wsPath, `{not valid json`)

	cfg, err := loadLocalWorkspaceConfig(wsPath)
	require.Error(t, err)
	require.Nil(t, cfg)
}

func TestDetectPackageManager_Yarn(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "yarn.lock"), []byte(""), 0o644))
	require.Equal(t, packageManagerYarn, detectPackageManager(dir))
}

func TestDetectPackageManager_NPM(t *testing.T) {
	require.Equal(t, packageManagerNpm, detectPackageManager(t.TempDir()))
}

func TestDefaultBundles_UnionsInWorkspaceOrder(t *testing.T) {
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	writeWorkspaceConfig(t, a, `{"default_bundles":["go"]}`)
	writeWorkspaceConfig(t, b, `{"default_bundles":["docs","go"]}`)

	require.Equal(t, []string{"go", "docs"}, localDefaultBundles(t, a, b, c))
	require.Nil(t, localDefaultBundles(t, c))
}

func TestDefaultBundles_SkipsUnreadableConfig(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	writeWorkspaceConfig(t, a, `{`)
	writeWorkspaceConfig(t, b, `{"default_bundles":["go"]}`)

	require.Equal(t, []string{"go"}, localDefaultBundles(t, a, b))
}
