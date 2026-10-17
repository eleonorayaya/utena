package claudesettings

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const globalConfigFileName = ".claude.json"

func ClaudeGlobalConfigPath() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, globalConfigFileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, globalConfigFileName), nil
}

var trustMu sync.Mutex

func TrustProject(globalConfigPath, projectPath string) error {
	trustMu.Lock()
	defer trustMu.Unlock()
	name := filepath.Base(globalConfigPath)
	return editJSONObject(globalConfigPath, privateFilePerm, true, func(raw map[string]any) error {
		projects, err := childObject(raw, "projects")
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		entry, err := childObject(projects, projectPath)
		if err != nil {
			return fmt.Errorf("%s: project %w", name, err)
		}
		if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
			entry["hasTrustDialogAccepted"] = true
		}
		return nil
	})
}
