package claudesettings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

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

func EnsureWorkspaceRoot(workspacePath string) error {
	claudeDir := filepath.Join(workspacePath, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}
	settingsPath := filepath.Join(claudeDir, "settings.local.json")
	if _, err := os.Lstat(settingsPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat settings.local.json: %w", err)
	}
	data, err := defaultSettingsJSON(workspacePath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		return fmt.Errorf("write settings.local.json: %w", err)
	}
	return nil
}

func LinkWorktree(workspacePath, worktreePath string) error {
	dstDir := filepath.Join(worktreePath, ".claude")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("create worktree .claude dir: %w", err)
	}
	dst := filepath.Join(dstDir, "settings.local.json")

	if _, err := os.Lstat(dst); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat worktree settings.local.json: %w", err)
	}

	src := filepath.Join(workspacePath, ".claude", "settings.local.json")
	rel, err := filepath.Rel(dstDir, src)
	if err != nil {
		return fmt.Errorf("compute relative path: %w", err)
	}
	if err := os.Symlink(rel, dst); err != nil {
		return fmt.Errorf("create symlink: %w", err)
	}
	return nil
}
