package git

import (
	"errors"

	"github.com/eleonorayaya/utena/internal/common"
	"gorm.io/gorm"
)

var (
	ErrBranchNotFound      = errors.New("branch not found")
	ErrBranchAlreadyExists = common.NewConflict("branch already exists")
)

type BranchStatus string

const (
	BranchStatusPending BranchStatus = "pending"
	BranchStatusTracked BranchStatus = "tracked"
	BranchStatusGone    BranchStatus = "gone"
)

type Branch struct {
	gorm.Model
	Name         string       `json:"name" gorm:"uniqueIndex:idx_branch_repo"`
	RepoID       uint         `json:"repo_id" gorm:"uniqueIndex:idx_branch_repo;index"`
	BaseBranchID *uint        `json:"base_branch_id,omitempty" gorm:"index"`
	BaseBranch   *Branch      `json:"base_branch,omitempty" gorm:"foreignKey:BaseBranchID"`
	ExistsLocal  bool         `json:"exists_local"`
	ExistsRemote bool         `json:"exists_remote"`
	IsDirty      bool         `json:"is_dirty"`
	Status       BranchStatus `json:"status" gorm:"index"`
}

func (b *Branch) GetStatus() BranchStatus  { return b.Status }
func (b *Branch) SetStatus(s BranchStatus) { b.Status = s }

func (b *Branch) DeriveStatus() BranchStatus {
	if b.ExistsLocal || b.ExistsRemote {
		return BranchStatusTracked
	}
	if b.Status == BranchStatusPending || b.Status == "" {
		return BranchStatusPending
	}
	return BranchStatusGone
}
