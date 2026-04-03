package git

import (
	"errors"
	"fmt"

	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

type RepoStore struct {
	db db.Database
}

func NewRepoStore(database db.Database) *RepoStore {
	return &RepoStore{
		db: database,
	}
}

func (s *RepoStore) Add(repo *Repo) error {
	if repo == nil {
		return errors.New("repo cannot be nil")
	}

	if err := s.db.Create(repo).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || db.IsUniqueConstraintError(err) {
			return fmt.Errorf("repo '%s' already exists: %w", repo.Path, ErrRepoAlreadyExists)
		}
		return err
	}
	return nil
}

func (s *RepoStore) GetByID(id uint) (*Repo, error) {
	var repo Repo
	if err := s.db.First(&repo, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepoNotFound
		}
		return nil, err
	}
	return &repo, nil
}

func (s *RepoStore) GetByPath(path string) (*Repo, error) {
	var repo Repo
	if err := s.db.First(&repo, "path = ?", path).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepoNotFound
		}
		return nil, err
	}
	return &repo, nil
}

func (s *RepoStore) GetByFullName(fullName string) (*Repo, error) {
	var repo Repo
	if err := s.db.First(&repo, "full_name = ?", fullName).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepoNotFound
		}
		return nil, err
	}
	return &repo, nil
}

func (s *RepoStore) List() []Repo {
	var repos []Repo
	s.db.Find(&repos)
	return repos
}

func (s *RepoStore) Update(repo *Repo) error {
	if repo == nil {
		return errors.New("repo cannot be nil")
	}
	if repo.ID == 0 {
		return errors.New("repo ID cannot be zero")
	}

	var existing Repo
	if err := s.db.First(&existing, "id = ?", repo.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRepoNotFound
		}
		return err
	}

	return s.db.Save(repo).Error
}

func (s *RepoStore) Upsert(repo *Repo) error {
	if repo == nil {
		return errors.New("repo cannot be nil")
	}

	var existing Repo
	if err := s.db.First(&existing, "path = ?", repo.Path).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return s.db.Create(repo).Error
		}
		return err
	}

	repo.ID = existing.ID
	return s.db.Save(repo).Error
}
