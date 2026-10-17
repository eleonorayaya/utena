package claudesettings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	settingsFileName         = "settings.local.json"
	mcpFileName              = ".mcp.json"
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

func readRawMap(path, label string) (map[string]any, []byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, false, fmt.Errorf("parse %s: %w", label, err)
	}
	if raw == nil {
		raw = map[string]any{}
	}
	return raw, data, true, nil
}

func writeRawMapIfChanged(path, label string, raw map[string]any, prev []byte) error {
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", label, err)
	}
	out = append(out, '\n')
	if bytes.Equal(out, prev) {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s dir: %w", filepath.Base(dir), err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func ApplyPlugins(rootPath string, managed, enabled []string) error {
	settingsPath := filepath.Join(rootPath, ".claude", settingsFileName)
	raw, data, exists, err := readRawMap(settingsPath, "settings")
	if err != nil {
		return err
	}
	if !exists && len(enabled) == 0 {
		return nil
	}

	plugins, _ := raw["enabledPlugins"].(map[string]any)
	if plugins == nil {
		plugins = make(map[string]any)
	}
	for _, id := range managed {
		delete(plugins, id)
	}
	for _, id := range enabled {
		plugins[id] = true
	}
	if len(plugins) == 0 {
		delete(raw, "enabledPlugins")
	} else {
		raw["enabledPlugins"] = plugins
	}
	return writeRawMapIfChanged(settingsPath, "settings", raw, data)
}

func ApplyMcpServers(rootPath string, managed []string, selected map[string]json.RawMessage) error {
	names := slices.Sorted(maps.Keys(selected))
	if err := applyMcpJSON(rootPath, managed, names, selected); err != nil {
		return err
	}
	return applyMcpApprovals(rootPath, managed, names)
}

func applyMcpJSON(rootPath string, managed, names []string, selected map[string]json.RawMessage) error {
	mcpPath := filepath.Join(rootPath, mcpFileName)
	raw, data, exists, err := readRawMap(mcpPath, mcpFileName)
	if err != nil {
		return err
	}
	if !exists && len(names) == 0 {
		return nil
	}

	servers, _ := raw["mcpServers"].(map[string]any)
	if servers == nil {
		servers = make(map[string]any)
	}
	for _, name := range managed {
		delete(servers, name)
	}
	for _, name := range names {
		var def any
		if err := json.Unmarshal(selected[name], &def); err != nil {
			return fmt.Errorf("parse mcp server %s: %w", name, err)
		}
		servers[name] = def
	}
	if len(servers) == 0 {
		delete(raw, "mcpServers")
	} else {
		raw["mcpServers"] = servers
	}
	return writeRawMapIfChanged(mcpPath, mcpFileName, raw, data)
}

func applyMcpApprovals(rootPath string, managed, names []string) error {
	settingsPath := filepath.Join(rootPath, ".claude", settingsFileName)
	raw, data, exists, err := readRawMap(settingsPath, "settings")
	if err != nil {
		return err
	}
	if !exists && len(names) == 0 {
		return nil
	}

	drop := make(map[string]struct{}, len(managed)+len(names))
	for _, name := range managed {
		drop[name] = struct{}{}
	}
	for _, name := range names {
		drop[name] = struct{}{}
	}
	existing, _ := raw["enabledMcpjsonServers"].([]any)
	approved := make([]any, 0, len(existing)+len(names))
	for _, entry := range existing {
		if s, ok := entry.(string); ok {
			if _, skip := drop[s]; skip {
				continue
			}
		}
		approved = append(approved, entry)
	}
	for _, name := range names {
		approved = append(approved, name)
	}
	if len(approved) == 0 {
		delete(raw, "enabledMcpjsonServers")
	} else {
		raw["enabledMcpjsonServers"] = approved
	}
	return writeRawMapIfChanged(settingsPath, "settings", raw, data)
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
