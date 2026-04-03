package git

import (
	"errors"
	"fmt"

	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

type PRStore struct {
	db db.Database
}

func NewPRStore(database db.Database) *PRStore {
	return &PRStore{
		db: database,
	}
}

func (s *PRStore) Add(pr *PullRequest) error {
	if pr == nil {
		return errors.New("pull request cannot be nil")
	}

	if err := s.db.Create(pr).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || db.IsUniqueConstraintError(err) {
			return fmt.Errorf("pull request #%d in repo %d already exists: %w", pr.Number, pr.RepoID, ErrPRAlreadyExists)
		}
		return err
	}
	return nil
}

func (s *PRStore) GetByID(id uint) (*PullRequest, error) {
	var pr PullRequest
	if err := s.db.First(&pr, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPRNotFound
		}
		return nil, err
	}
	return &pr, nil
}

func (s *PRStore) GetByRepoAndNumber(repoID uint, number int) (*PullRequest, error) {
	var pr PullRequest
	if err := s.db.First(&pr, "repo_id = ? AND number = ?", repoID, number).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPRNotFound
		}
		return nil, err
	}
	return &pr, nil
}

func (s *PRStore) ListByRepo(repoID uint) []PullRequest {
	var prs []PullRequest
	s.db.Find(&prs, "repo_id = ?", repoID)
	return prs
}

func (s *PRStore) ListByBranch(headBranchID uint) []PullRequest {
	var prs []PullRequest
	s.db.Find(&prs, "head_branch_id = ?", headBranchID)
	return prs
}

func (s *PRStore) ListByState(repoID uint, state PRState) []PullRequest {
	var prs []PullRequest
	s.db.Where("repo_id = ? AND state = ?", repoID, state).Find(&prs)
	return prs
}

func (s *PRStore) Upsert(pr *PullRequest) error {
	if pr == nil {
		return errors.New("pull request cannot be nil")
	}

	var existing PullRequest
	if err := s.db.First(&existing, "repo_id = ? AND number = ?", pr.RepoID, pr.Number).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return s.db.Create(pr).Error
		}
		return err
	}

	pr.ID = existing.ID
	return s.db.Save(pr).Error
}

func (s *PRStore) Update(pr *PullRequest) error {
	if pr == nil {
		return errors.New("pull request cannot be nil")
	}
	if pr.ID == 0 {
		return errors.New("pull request ID cannot be zero")
	}

	var existing PullRequest
	if err := s.db.First(&existing, "id = ?", pr.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPRNotFound
		}
		return err
	}

	return s.db.Save(pr).Error
}
