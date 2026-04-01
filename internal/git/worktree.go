package git

import (
	"errors"

	"gorm.io/gorm"
)

var (
	ErrWorktreeNotFound      = errors.New("worktree not found")
	ErrWorktreeAlreadyExists = errors.New("worktree already exists")
)

type Worktree struct {
	gorm.Model
	Path     string `json:"path" gorm:"uniqueIndex"`
	BranchID uint   `json:"branch_id" gorm:"uniqueIndex"`
	RepoID   uint   `json:"repo_id" gorm:"index"`
}
