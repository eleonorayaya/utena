package workspace

import (
	"time"

	"github.com/eleonorayaya/utena/internal/git"
	"gorm.io/gorm"
)

type WorkspaceStatus string

const (
	StatusReady     WorkspaceStatus = "ready"
	StatusCloning   WorkspaceStatus = "cloning"
	StatusMigrating WorkspaceStatus = "migrating"
	StatusFailed    WorkspaceStatus = "failed"
)

type Workspace struct {
	gorm.Model
	Name        string          `json:"name"`
	Path        string          `json:"path" gorm:"uniqueIndex"`
	IsGitRepo   bool            `json:"is_git_repo"`
	IsBare      bool            `json:"is_bare" gorm:"default:false"`
	IsHidden    bool            `json:"is_hidden" gorm:"default:false"`
	RepoID      *uint           `json:"repo_id,omitempty" gorm:"uniqueIndex"`
	Repo        *git.Repo       `json:"repo,omitempty" gorm:"foreignKey:RepoID"`
	LastUsedAt  time.Time       `json:"last_used_at,omitempty"`
	Status      WorkspaceStatus `json:"status" gorm:"default:ready"`
	StatusError string          `json:"status_error,omitempty"`
	Progress    string          `json:"progress,omitempty" gorm:"-"`
}

// AfterFind normalizes a legacy empty status (rows created before the status
// column existed) to ready on every load, so no read path can surface "".
func (w *Workspace) AfterFind(*gorm.DB) error {
	if w.Status == "" {
		w.Status = StatusReady
	}
	return nil
}

func (w *Workspace) IsBusy() bool {
	return w.Status == StatusCloning || w.Status == StatusMigrating
}
