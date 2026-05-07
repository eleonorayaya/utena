package session

import (
	"errors"
	"fmt"

	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

type SessionWorkspaceStore struct {
	db db.Database
}

func NewSessionWorkspaceStore(database db.Database) *SessionWorkspaceStore {
	return &SessionWorkspaceStore{db: database}
}

func (s *SessionWorkspaceStore) Add(sw *SessionWorkspace) error {
	if sw == nil {
		return errors.New("session workspace cannot be nil")
	}
	if err := s.db.Omit("Workspace", "GitBranch").Create(sw).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || db.IsUniqueConstraintError(err) {
			return fmt.Errorf("session workspace already exists: %w", err)
		}
		return err
	}
	return nil
}

func (s *SessionWorkspaceStore) Update(sw *SessionWorkspace) error {
	if sw == nil {
		return errors.New("session workspace cannot be nil")
	}
	if sw.ID == 0 {
		return errors.New("session workspace ID cannot be zero")
	}
	return s.db.Omit("Workspace", "GitBranch").Save(sw).Error
}

func (s *SessionWorkspaceStore) ListBySessionID(sessionID uint) ([]SessionWorkspace, error) {
	var rows []SessionWorkspace
	if err := s.db.Joins("Workspace").Joins("GitBranch").Where("session_workspaces.session_id = ?", sessionID).Order("session_workspaces.position ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *SessionWorkspaceStore) DeleteBySessionID(sessionID uint) error {
	return s.db.Where("session_id = ?", sessionID).Delete(&SessionWorkspace{}).Error
}
