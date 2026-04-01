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
			return nil, nil
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
	var worktree Worktree
	if err := s.db.First(&worktree, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrWorktreeNotFound
		}
		return err
	}

	return s.db.Delete(&Worktree{}, id).Error
}

func (s *WorktreeStore) DeleteByBranchID(branchID uint) error {
	result := s.db.Where("branch_id = ?", branchID).Delete(&Worktree{})
	return result.Error
}

func (s *WorktreeStore) ListByRepo(repoID uint) []Worktree {
	var worktrees []Worktree
	s.db.Find(&worktrees, "repo_id = ?", repoID)
	return worktrees
}
