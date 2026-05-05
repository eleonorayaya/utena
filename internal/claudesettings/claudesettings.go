package claudesettings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const settingsFileName = "settings.local.json"

type settingsLocal struct {
	Sandbox struct {
		Filesystem struct {
			AllowWrite []string `json:"allowWrite"`
		} `json:"filesystem"`
	} `json:"sandbox"`
}

func defaultSettingsJSON(workspacePath string) ([]byte, error) {
	var s settingsLocal
	s.Sandbox.Filesystem.AllowWrite = []string{filepath.Join(workspacePath, ".git")}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal settings: %w", err)
	}
	return append(data, '\n'), nil
}

func settingsFileExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("stat %s: %w", filepath.Base(path), err)
}

func EnsureWorkspaceRoot(workspacePath string) error {
	claudeDir := filepath.Join(workspacePath, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}
	settingsPath := filepath.Join(claudeDir, settingsFileName)
	exists, err := settingsFileExists(settingsPath)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	data, err := defaultSettingsJSON(workspacePath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", settingsFileName, err)
	}
	return nil
}

func LinkWorktree(workspacePath, worktreePath string) error {
	dstDir := filepath.Join(worktreePath, ".claude")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("create worktree .claude dir: %w", err)
	}
	dst := filepath.Join(dstDir, settingsFileName)
	exists, err := settingsFileExists(dst)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	src := filepath.Join(workspacePath, ".claude", settingsFileName)
	rel, err := filepath.Rel(dstDir, src)
	if err != nil {
		return fmt.Errorf("compute relative path: %w", err)
	}
	if err := os.Symlink(rel, dst); err != nil {
		return fmt.Errorf("create symlink: %w", err)
	}
	return nil
}
