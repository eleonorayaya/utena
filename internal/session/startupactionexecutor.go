package session

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

type WindowSpawner interface {
	SpawnWindow(sessionName, startDir, command string) error
}

func executeStartupActions(actions []StartupAction, spawner WindowSpawner, tmuxName, startDir string) {
	for _, action := range actions {
		if err := executeStartupAction(action, spawner, tmuxName, startDir); err != nil {
			slog.Warn("startup action failed", "type", action.Type, "session", tmuxName, "error", err)
		}
	}
}

func executeStartupAction(action StartupAction, spawner WindowSpawner, tmuxName, startDir string) error {
	switch action.Type {
	case StartupActionTypeClaude:
		var opts ClaudeActionOptions
		if err := json.Unmarshal([]byte(action.Options), &opts); err != nil {
			return fmt.Errorf("invalid claude options: %w", err)
		}
		return spawner.SpawnWindow(tmuxName, startDir, fmt.Sprintf("claude %q", opts.Prompt))
	default:
		slog.Warn("unknown startup action type, skipping", "type", action.Type)
		return nil
	}
}
