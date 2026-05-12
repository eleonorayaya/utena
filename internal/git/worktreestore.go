package git

import (
	"errors"
	"fmt"
	"strings"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

// worktreeConflictMessage produces a friendly message for a unique-constraint
// violation on the Worktree table. SQLite reports which column failed; we
// surface either the path or the branch as the conflicting concept so the
// user knows what to free up.
func worktreeConflictMessage(wt *Worktree, err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "worktrees.path"):
		return fmt.Sprintf("a worktree at path %q already exists — remove it first or pick a different path", wt.Path)
	case strings.Contains(msg, "worktrees.branch_id"):
		return fmt.Sprintf("branch %d already has a worktree — remove the existing worktree before creating another", wt.BranchID)
	default:
		return fmt.Sprintf("worktree at path %q already exists", wt.Path)
	}
}

type WorktreeStore struct {
	db db.Database
}

func NewWorktreeStore(database db.Database) *WorktreeStore {
	return &WorktreeStore{
		db: database,
	}
}

func (s *WorktreeStore) Add(worktree *Worktree) error {
	if worktree == nil {
		return errors.New("worktree cannot be nil")
	}

	if err := s.db.Create(worktree).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || db.IsUniqueConstraintError(err) {
			return common.WrapConflict(worktreeConflictMessage(worktree, err), ErrWorktreeAlreadyExists)
		}
		return err
	}
	return nil
}

func (s *WorktreeStore) Update(worktree *Worktree) error {
	if worktree == nil {
		return errors.New("worktree cannot be nil")
	}
	if worktree.ID == 0 {
		return errors.New("worktree ID cannot be zero")
	}
	if err := s.db.Save(worktree).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || db.IsUniqueConstraintError(err) {
			return common.WrapConflict(worktreeConflictMessage(worktree, err), ErrWorktreeAlreadyExists)
		}
		return err
	}
	return nil
}

func (s *WorktreeStore) GetByID(id uint) (*Worktree, error) {
	var worktree Worktree
	if err := s.db.First(&worktree, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWorktreeNotFound
		}
		return nil, err
	}
	return &worktree, nil
}

func (s *WorktreeStore) GetByBranchID(branchID uint) (*Worktree, error) {
	var worktree Worktree
	if err := s.db.First(&worktree, "branch_id = ?", branchID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWorktreeNotFound
		}
		return nil, err
	}
	return &worktree, nil
}

func (s *WorktreeStore) GetByPath(path string) (*Worktree, error) {
	var worktree Worktree
	if err := s.db.First(&worktree, "path = ?", path).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWorktreeNotFound
		}
		return nil, err
	}
	return &worktree, nil
}

func (s *WorktreeStore) Delete(id uint) error {
	result := s.db.Where("id = ?", id).Unscoped().Delete(&Worktree{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrWorktreeNotFound
	}
	return nil
}

func (s *WorktreeStore) DeleteByBranchID(branchID uint) error {
	return s.db.Where("branch_id = ?", branchID).Unscoped().Delete(&Worktree{}).Error
}

func (s *WorktreeStore) DeleteByRepoID(repoID uint) error {
	return s.db.Where("repo_id = ?", repoID).Unscoped().Delete(&Worktree{}).Error
}

func (s *WorktreeStore) ListByRepo(repoID uint) []Worktree {
	var worktrees []Worktree
	s.db.Find(&worktrees, "repo_id = ?", repoID)
	return worktrees
}

// BackfillStatus migrates legacy rows to the present state. Pre-state-machine
// code only inserted Worktree records after a successful on-disk
// `git worktree add` (see GitService.ensureWorktreeRecord), so legacy rows
// with empty Status correspond to worktrees that were observed present at
// insert time. Subsequent reconciliation will demote any that have since
// disappeared on disk.
func (s *WorktreeStore) BackfillStatus() error {
	return s.db.Exec(
		"UPDATE worktrees SET status = ? WHERE status IS NULL OR status = ''",
		string(WorktreeStatusPresent),
	).Error
}
