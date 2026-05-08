package git

import (
	"errors"
	"fmt"

	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

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
			return fmt.Errorf("worktree already exists: %w", ErrWorktreeAlreadyExists)
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
	return s.db.Save(worktree).Error
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
