package session

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

type WindowSpawner interface {
	SpawnWindow(sessionName, startDir, command string) error
}

func executeSessionActions(actions []SessionAction, spawner WindowSpawner, store *SessionActionStore, tmuxName, startDir string) {
	for i := range actions {
		action := &actions[i]
		var err error
		var known bool
		switch action.Type {
		case SessionActionTypeClaude:
			known = true
			var opts ClaudeActionOptions
			if err = json.Unmarshal([]byte(action.Options), &opts); err != nil {
				err = fmt.Errorf("invalid claude options: %w", err)
			} else {
				err = spawner.SpawnWindow(tmuxName, startDir, fmt.Sprintf("claude %q", opts.Prompt))
			}
		default:
			slog.Warn("unknown session action type, skipping", "type", action.Type)
		}
		if !known {
			continue
		}
		if err != nil {
			slog.Warn("session action failed", "type", action.Type, "session", tmuxName, "error", err)
			store.UpdateError(action, err.Error())
		} else if action.Error != "" {
			store.UpdateError(action, "")
		}
	}
}
