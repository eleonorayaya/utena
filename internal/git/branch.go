package git

import (
	"errors"

	"gorm.io/gorm"
)

var (
	ErrBranchNotFound      = errors.New("branch not found")
	ErrBranchAlreadyExists = errors.New("branch already exists")
)

type Branch struct {
	gorm.Model
	Name         string  `json:"name" gorm:"uniqueIndex:idx_branch_repo"`
	RepoID       uint    `json:"repo_id" gorm:"uniqueIndex:idx_branch_repo;index"`
	BaseBranchID *uint   `json:"base_branch_id,omitempty" gorm:"index"`
	BaseBranch   *Branch `json:"base_branch,omitempty" gorm:"foreignKey:BaseBranchID"`
	ExistsLocal  bool    `json:"exists_local"`
	ExistsRemote bool    `json:"exists_remote"`
	IsDirty      bool    `json:"is_dirty"`
}
