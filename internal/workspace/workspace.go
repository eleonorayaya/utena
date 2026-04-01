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
	RepoID     *uint     `json:"repo_id,omitempty" gorm:"index"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
}
