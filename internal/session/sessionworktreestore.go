package session

import (
	"errors"
	"fmt"

	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

type SessionWorktreeStore struct {
	db db.Database
}

func NewSessionWorktreeStore(database db.Database) *SessionWorktreeStore {
	return &SessionWorktreeStore{db: database}
}

func (s *SessionWorktreeStore) Add(swt *SessionWorktree) error {
	if swt == nil {
		return errors.New("session worktree cannot be nil")
	}
	if err := s.db.Omit("Worktree").Create(swt).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || db.IsUniqueConstraintError(err) {
			return fmt.Errorf("session worktree already exists: %w", err)
		}
		return err
	}
	return nil
}

func (s *SessionWorktreeStore) Update(swt *SessionWorktree) error {
	if swt == nil {
		return errors.New("session worktree cannot be nil")
	}
	if swt.ID == 0 {
		return errors.New("session worktree ID cannot be zero")
	}
	return s.db.Omit("Worktree").Save(swt).Error
}

func (s *SessionWorktreeStore) ListBySessionID(sessionID uint) ([]SessionWorktree, error) {
	var rows []SessionWorktree
	if err := s.db.Where("session_id = ?", sessionID).Order("position ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *SessionWorktreeStore) DeleteBySessionID(sessionID uint) error {
	return s.db.Where("session_id = ?", sessionID).Delete(&SessionWorktree{}).Error
}
