package session

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/eleonorayaya/utena/internal/bundles"
)

type WorkspaceSetupActionType string

const (
	WorkspaceActionCopyFile    WorkspaceSetupActionType = "copy_file"
	WorkspaceActionInstallDeps WorkspaceSetupActionType = "install_deps"
)

type DestRelativeTo string

const (
	DestRelativeToWorkspaceDir DestRelativeTo = "workspace_dir"
	DestRelativeToSessionRoot  DestRelativeTo = "session_root"
)

const (
	packageManagerYarn = "yarn"
	packageManagerNpm  = "npm"
	packageManagerAuto = "auto"
)

type WorkspaceSetupAction struct {
	Type           WorkspaceSetupActionType `json:"type"`
	Src            string                   `json:"src,omitempty"`
	Dest           string                   `json:"dest,omitempty"`
	DestRelativeTo DestRelativeTo           `json:"dest_relative_to,omitempty"`
	PackageManager string                   `json:"package_manager,omitempty"`
	RunWith        []string                 `json:"run_with,omitempty"`
}

type WorkspaceConfig struct {
	SetupActions   []WorkspaceSetupAction `json:"setup_actions"`
	DefaultBundles []string               `json:"default_bundles,omitempty"`
}

func defaultBundles(wsPaths []string) []string {
	lists := make([][]string, 0, len(wsPaths))
	for _, p := range wsPaths {
		cfg, err := loadWorkspaceConfig(p)
		if err != nil {
			slog.Warn("failed to load workspace config", "path", p, "error", err)
			continue
		}
		if cfg != nil {
			lists = append(lists, cfg.DefaultBundles)
		}
	}
	return bundles.Union(lists...)
}

func defaultBundlesFrom(wss []stepWorkspace, configs map[uint]*WorkspaceConfig) []string {
	lists := make([][]string, 0, len(wss))
	for _, ws := range wss {
		if cfg := configs[ws.ID]; cfg != nil {
			lists = append(lists, cfg.DefaultBundles)
		}
	}
	return bundles.Union(lists...)
}

func loadWorkspaceConfig(wsPath string) (*WorkspaceConfig, error) {
	path := filepath.Join(wsPath, ".utena", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var cfg WorkspaceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func detectPackageManager(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "yarn.lock")); err == nil {
		return packageManagerYarn
	}
	return packageManagerNpm
}
