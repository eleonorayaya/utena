package session

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
}

type WorkspaceConfig struct {
	SetupActions []WorkspaceSetupAction `json:"setup_actions"`
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
