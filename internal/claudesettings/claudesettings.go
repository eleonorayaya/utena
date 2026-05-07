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

func defaultAllowWritePaths(workspacePath string) []string {
	return []string{
		filepath.Join(workspacePath, ".git"),
		filepath.Join(workspacePath, ".bare"),
	}
}

func defaultSettingsJSON(workspacePath string) ([]byte, error) {
	var s settingsLocal
	s.Sandbox.Filesystem.AllowWrite = defaultAllowWritePaths(workspacePath)
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

func mergeAllowWrite(data []byte, paths []string) ([]byte, bool, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false, fmt.Errorf("parse settings: %w", err)
	}
	sandbox, _ := raw["sandbox"].(map[string]any)
	if sandbox == nil {
		sandbox = make(map[string]any)
		raw["sandbox"] = sandbox
	}
	filesystem, _ := sandbox["filesystem"].(map[string]any)
	if filesystem == nil {
		filesystem = make(map[string]any)
		sandbox["filesystem"] = filesystem
	}
	allowWrite, _ := filesystem["allowWrite"].([]any)

	existing := make(map[string]struct{}, len(allowWrite))
	for _, entry := range allowWrite {
		if s, ok := entry.(string); ok {
			existing[s] = struct{}{}
		}
	}

	changed := false
	for _, p := range paths {
		if _, ok := existing[p]; ok {
			continue
		}
		allowWrite = append(allowWrite, p)
		existing[p] = struct{}{}
		changed = true
	}
	if !changed {
		return nil, false, nil
	}
	filesystem["allowWrite"] = allowWrite
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("marshal settings: %w", err)
	}
	return append(out, '\n'), true, nil
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
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", settingsFileName, err)
		}
		merged, changed, err := mergeAllowWrite(data, defaultAllowWritePaths(workspacePath))
		if err != nil || !changed {
			return err
		}
		if err := os.WriteFile(settingsPath, merged, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", settingsFileName, err)
		}
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
