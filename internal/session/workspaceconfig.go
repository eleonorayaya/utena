package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/eleonorayaya/utena/internal/bundles"
	"github.com/eleonorayaya/utena/internal/workspace"
)

const workspaceConfigRelPath = ".utena/config.json"

type sourceReadError struct {
	err error
}

func (e *sourceReadError) Error() string { return e.err.Error() }

func (e *sourceReadError) Unwrap() error { return e.err }

func isSourceReadError(err error) bool {
	var e *sourceReadError
	return errors.As(err, &e)
}

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

func defaultBundles(ctx context.Context, reader workspace.SourceReader, wss []workspace.Workspace) ([]string, error) {
	lists := make([][]string, 0, len(wss))
	for _, ws := range wss {
		cfg, err := loadWorkspaceConfig(ctx, reader, ws)
		if isSourceReadError(err) {
			return nil, err
		}
		if err != nil {
			slog.Warn("failed to load workspace config", "path", ws.Path, "error", err)
			continue
		}
		if cfg != nil {
			lists = append(lists, cfg.DefaultBundles)
		}
	}
	return bundles.Union(lists...), nil
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

func loadWorkspaceConfig(ctx context.Context, reader workspace.SourceReader, ws workspace.Workspace) (*WorkspaceConfig, error) {
	data, _, err := reader.ReadSourceFile(ctx, ws, workspaceConfigRelPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, &sourceReadError{err: fmt.Errorf("read %s for workspace %q: %w", workspaceConfigRelPath, ws.Name, err)}
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
