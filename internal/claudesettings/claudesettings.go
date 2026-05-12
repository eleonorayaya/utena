package claudesettings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	settingsFileName         = "settings.local.json"
	multiSessionGuideName    = "CLAUDE.md"
	multiSessionGuideMarker  = "<!-- utena:multi-session-guide -->"
	multiSessionGuideContent = multiSessionGuideMarker + `
# Session checkouts

The directories below are the **only** canonical copies of these projects for this session. Read from and edit them here; do not look at or modify any other copy of these projects elsewhere on this filesystem.

%s`
)

type SessionCheckout struct {
	Subdir        string
	WorkspaceName string
}

// EnsureMultiSessionGuide writes a CLAUDE.md at the session root telling
// Claude that the listed subdirectory checkouts are the canonical copies
// for this session. Skips if the file already exists and wasn't authored
// by utena (no marker), so the user's customizations stay intact.
func EnsureMultiSessionGuide(sessionRoot string, checkouts []SessionCheckout) error {
	path := filepath.Join(sessionRoot, multiSessionGuideName)
	if existing, err := os.ReadFile(path); err == nil {
		if !strings.Contains(string(existing), multiSessionGuideMarker) {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", multiSessionGuideName, err)
	}
	var list strings.Builder
	for _, c := range checkouts {
		fmt.Fprintf(&list, "- `%s/` — workspace `%s`\n", c.Subdir, c.WorkspaceName)
	}
	body := fmt.Sprintf(multiSessionGuideContent, list.String())
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", multiSessionGuideName, err)
	}
	return nil
}

type settingsLocal struct {
	Sandbox struct {
		Filesystem struct {
			AllowWrite []string `json:"allowWrite"`
		} `json:"filesystem"`
	} `json:"sandbox"`
}

func WorkspaceAllowWritePaths(workspacePath string) []string {
	return []string{
		filepath.Join(workspacePath, ".git"),
		filepath.Join(workspacePath, ".bare"),
	}
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
	return ensureSettingsFile(workspacePath, WorkspaceAllowWritePaths(workspacePath))
}

func EnsureSessionRoot(sessionRoot string, workspacePaths []string) error {
	allowWrite := make([]string, 0, len(workspacePaths)*2)
	for _, p := range workspacePaths {
		allowWrite = append(allowWrite, WorkspaceAllowWritePaths(p)...)
	}
	return ensureSettingsFile(sessionRoot, allowWrite)
}

func ensureSettingsFile(rootPath string, allowWrite []string) error {
	claudeDir := filepath.Join(rootPath, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}
	settingsPath := filepath.Join(claudeDir, settingsFileName)
	exists, err := settingsFileExists(settingsPath)
	if err != nil {
		return err
	}
	if !exists {
		var s settingsLocal
		s.Sandbox.Filesystem.AllowWrite = append([]string{}, allowWrite...)
		data, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal settings: %w", err)
		}
		data = append(data, '\n')
		if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", settingsFileName, err)
		}
		return nil
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", settingsFileName, err)
	}
	merged, changed, err := mergeAllowWrite(data, allowWrite)
	if err != nil || !changed {
		return err
	}
	if err := os.WriteFile(settingsPath, merged, 0o644); err != nil {
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
