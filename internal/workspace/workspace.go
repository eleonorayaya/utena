package workspace

import "time"

type Workspace struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	IsGitRepo  bool      `json:"is_git_repo"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
}
