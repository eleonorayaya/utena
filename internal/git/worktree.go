package git

import (
	"errors"

	"github.com/eleonorayaya/utena/internal/common"
	"gorm.io/gorm"
)

var (
	ErrWorktreeNotFound      = errors.New("worktree not found")
	ErrWorktreeAlreadyExists = common.NewConflict("worktree already exists")
)

type WorktreeStatus string

const (
	WorktreeStatusPending WorktreeStatus = "pending"
	WorktreeStatusPresent WorktreeStatus = "present"
	WorktreeStatusMissing WorktreeStatus = "missing"
)

type Worktree struct {
	gorm.Model
	Path     string         `json:"path" gorm:"uniqueIndex"`
	BranchID uint           `json:"branch_id" gorm:"uniqueIndex"`
	RepoID   uint           `json:"repo_id" gorm:"index"`
	Status   WorktreeStatus `json:"status" gorm:"index"`
	Branch   *Branch        `json:"branch,omitempty" gorm:"foreignKey:BranchID"`
}

func (w *Worktree) GetStatus() WorktreeStatus  { return w.Status }
func (w *Worktree) SetStatus(s WorktreeStatus) { w.Status = s }
