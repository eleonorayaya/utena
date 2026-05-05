package claudesettings

import (
	"encoding/json"
	"errors"
	"fmt"
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
	return errors.New("not implemented")
}

func LinkWorktree(workspacePath, worktreePath string) error {
	return errors.New("not implemented")
}
