package workspace

import (
	"time"

	"gorm.io/gorm"
)

type Workspace struct {
	gorm.Model
	Name       string    `json:"name"`
	Path       string    `json:"path" gorm:"uniqueIndex"`
	IsGitRepo  bool      `json:"is_git_repo"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
}
