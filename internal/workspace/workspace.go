package workspace

import (
	"time"

	"github.com/eleonorayaya/utena/internal/git"
	"gorm.io/gorm"
)

type Workspace struct {
	gorm.Model
	Name       string    `json:"name"`
	Path       string    `json:"path" gorm:"uniqueIndex"`
	IsGitRepo  bool      `json:"is_git_repo"`
	IsBare     bool      `json:"is_bare" gorm:"default:false"`
	IsHidden   bool      `json:"is_hidden" gorm:"default:false"`
	RepoID     *uint     `json:"repo_id,omitempty" gorm:"uniqueIndex"`
	Repo       *git.Repo `json:"repo,omitempty" gorm:"foreignKey:RepoID"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
}
