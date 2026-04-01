package git

import (
	"errors"
	"fmt"

	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

type BranchStore struct {
	db db.Database
}

func NewBranchStore(database db.Database) *BranchStore {
	return &BranchStore{
		db: database,
	}
}

func (s *BranchStore) Add(branch *Branch) error {
	if branch == nil {
		return errors.New("branch cannot be nil")
	}

	if err := s.db.Create(branch).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || isUniqueConstraintError(err) {
			return fmt.Errorf("branch '%s' in repo %d already exists: %w", branch.Name, branch.RepoID, ErrBranchAlreadyExists)
		}
		return err
	}
	return nil
}

func (s *BranchStore) GetByID(id uint) (*Branch, error) {
	var branch Branch
	if err := s.db.First(&branch, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBranchNotFound
		}
		return nil, err
	}
	return &branch, nil
}

func (s *BranchStore) GetByNameAndRepo(name string, repoID uint) (*Branch, error) {
	var branch Branch
	if err := s.db.First(&branch, "name = ? AND repo_id = ?", name, repoID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBranchNotFound
		}
		return nil, err
	}
	return &branch, nil
}

func (s *BranchStore) ListByRepo(repoID uint) []Branch {
	var branches []Branch
	s.db.Find(&branches, "repo_id = ?", repoID)
	return branches
}

func (s *BranchStore) Update(branch *Branch) error {
	if branch == nil {
		return errors.New("branch cannot be nil")
	}
	if branch.ID == 0 {
		return errors.New("branch ID cannot be zero")
	}

	var existing Branch
	if err := s.db.First(&existing, "id = ?", branch.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrBranchNotFound
		}
		return err
	}

	return s.db.Save(branch).Error
}

func (s *BranchStore) Upsert(branch *Branch) error {
	if branch == nil {
		return errors.New("branch cannot be nil")
	}

	var existing Branch
	if err := s.db.First(&existing, "name = ? AND repo_id = ?", branch.Name, branch.RepoID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return s.db.Create(branch).Error
		}
		return err
	}

	branch.ID = existing.ID
	return s.db.Save(branch).Error
}

func (s *BranchStore) Delete(id uint) error {
	var branch Branch
	if err := s.db.First(&branch, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrBranchNotFound
		}
		return err
	}

	return s.db.Delete(&Branch{}, id).Error
}
