package session

import (
	"errors"
	"fmt"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

var ErrSessionWorktreeAlreadyExists = common.NewConflict("session is already linked to this worktree")

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
			return common.WrapConflict(
				fmt.Sprintf("session %d is already linked to worktree %d", swt.SessionID, swt.WorktreeID),
				ErrSessionWorktreeAlreadyExists,
			)
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
	if err := s.db.Omit("Worktree").Save(swt).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || db.IsUniqueConstraintError(err) {
			return common.WrapConflict(
				fmt.Sprintf("session %d is already linked to worktree %d", swt.SessionID, swt.WorktreeID),
				ErrSessionWorktreeAlreadyExists,
			)
		}
		return err
	}
	return nil
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

// HardDeleteByWorktreeRepoID bypasses GORM's soft delete because SQLite's
// FK check sees physical rows regardless of deleted_at — soft-deleted join
// rows would still block deleting the worktrees they point at.
func (s *SessionWorktreeStore) HardDeleteByWorktreeRepoID(repoID uint) error {
	return s.db.Exec(
		"DELETE FROM session_worktrees WHERE worktree_id IN (SELECT id FROM worktrees WHERE repo_id = ?)",
		repoID,
	).Error
}
