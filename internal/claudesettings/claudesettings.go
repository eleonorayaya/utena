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

func WorkspaceAllowWritePaths(workspacePath string) []string {
	return []string{
		filepath.Join(workspacePath, ".git"),
		filepath.Join(workspacePath, ".bare"),
	}
}

func settingsPath(rootPath string) string {
	return filepath.Join(rootPath, ".claude", settingsFileName)
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

func mergeAllowWrite(raw map[string]any, paths []string) {
	sandbox, _ := raw["sandbox"].(map[string]any)
	filesystem, _ := sandbox["filesystem"].(map[string]any)
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
		return
	}
	if sandbox == nil {
		sandbox = make(map[string]any)
		raw["sandbox"] = sandbox
	}
	if filesystem == nil {
		filesystem = make(map[string]any)
		sandbox["filesystem"] = filesystem
	}
	filesystem["allowWrite"] = allowWrite
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
	return editJSONObject(settingsPath(rootPath), projectFilePerm, true, func(raw map[string]any) error {
		mergeAllowWrite(raw, allowWrite)
		return nil
	})
}

func replaceManaged(raw map[string]any, field string, managed []string, set map[string]any) {
	current, _ := raw[field].(map[string]any)
	if current == nil {
		current = make(map[string]any)
	}
	for _, k := range managed {
		delete(current, k)
	}
	maps.Copy(current, set)
	if len(current) == 0 {
		delete(raw, field)
		return
	}
	raw[field] = current
}

func replaceApprovals(raw map[string]any, managed, names []string) {
	drop := make(map[string]struct{}, len(managed)+len(names))
	for _, name := range slices.Concat(managed, names) {
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
		return
	}
	raw["enabledMcpjsonServers"] = approved
}

type BundleSettings struct {
	ManagedPlugins []string
	Plugins        []string
	ManagedServers []string
	Servers        map[string]json.RawMessage
}

func ApplyBundles(rootPath string, b BundleSettings) error {
	names := slices.Sorted(maps.Keys(b.Servers))
	servers := make(map[string]any, len(names))
	for _, name := range names {
		if err := validateMcpDefinition(b.Servers[name]); err != nil {
			return fmt.Errorf("mcp server %s: %w", name, err)
		}
		servers[name] = b.Servers[name]
	}
	plugins := make(map[string]any, len(b.Plugins))
	for _, id := range b.Plugins {
		plugins[id] = true
	}

	mcpErr := editJSONObject(filepath.Join(rootPath, mcpFileName), projectFilePerm, false, func(raw map[string]any) error {
		replaceManaged(raw, "mcpServers", b.ManagedServers, servers)
		return nil
	})
	settingsErr := editJSONObject(settingsPath(rootPath), projectFilePerm, false, func(raw map[string]any) error {
		replaceManaged(raw, "enabledPlugins", b.ManagedPlugins, plugins)
		if mcpErr == nil {
			replaceApprovals(raw, b.ManagedServers, names)
		}
		return nil
	})
	switch {
	case mcpErr != nil && settingsErr != nil:
		return fmt.Errorf("%w; %w", mcpErr, settingsErr)
	case mcpErr != nil:
		return mcpErr
	}
	return settingsErr
}

func validateMcpDefinition(def json.RawMessage) error {
	if !json.Valid(def) {
		return errors.New("invalid JSON definition")
	}
	if trimmed := bytes.TrimSpace(def); len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("definition must be a JSON object")
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
