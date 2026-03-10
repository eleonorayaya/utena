package workspace

import "time"

type Workspace struct {
	ID         string    `json:"id" gorm:"primaryKey"`
	Name       string    `json:"name"`
	Path       string    `json:"path" gorm:"uniqueIndex"`
	IsGitRepo  bool      `json:"is_git_repo"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
}
