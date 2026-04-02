package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

type SessionStore struct {
	db db.Database
}

func NewSessionStore(database db.Database) *SessionStore {
	return &SessionStore{
		db: database,
	}
}

func (s *SessionStore) GetByID(id uint) (*Session, error) {
	var session Session
	if err := s.db.Joins("Workspace").Joins("GitBranch").Joins("TmuxSession").First(&session, "sessions.id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return &session, nil
}


func (s *SessionStore) List() []Session {
	var sessions []Session
	s.db.Joins("Workspace").Joins("GitBranch").Joins("TmuxSession").Preload("ClaudeSessions").Order("sessions.last_used_at DESC").Find(&sessions)
	return sessions
}

func (s *SessionStore) ListByWorkspace(workspaceID uint) []Session {
	var sessions []Session
	s.db.Joins("Workspace").Joins("GitBranch").Joins("TmuxSession").Preload("ClaudeSessions").Where("sessions.workspace_id = ?", workspaceID).Order("sessions.last_used_at DESC").Find(&sessions)
	return sessions
}

func (s *SessionStore) Add(session *Session) error {
	if session == nil {
		return errors.New("session cannot be nil")
	}

	if err := s.db.Omit("Workspace", "ClaudeSessions", "GitBranch", "TmuxSession").Create(session).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || db.IsUniqueConstraintError(err) {
			return fmt.Errorf("session '%s' already exists: %w", session.Name, ErrSessionAlreadyExists)
		}
		return err
	}
	return nil
}

func (s *SessionStore) Update(session *Session) error {
	if session == nil {
		return errors.New("session cannot be nil")
	}
	if session.ID == 0 {
		return errors.New("session ID cannot be zero")
	}

	var existing Session
	if err := s.db.First(&existing, "id = ?", session.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSessionNotFound
		}
		return err
	}

	return s.db.Omit("Workspace", "ClaudeSessions", "GitBranch", "TmuxSession").Save(session).Error
}

func (s *SessionStore) Delete(id uint) error {
	if id == 0 {
		return errors.New("session ID cannot be zero")
	}

	var existing Session
	if err := s.db.First(&existing, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSessionNotFound
		}
		return err
	}

	return s.db.Delete(&Session{}, "id = ?", id).Error
}

func (s *SessionStore) GetByBranchID(branchID uint) (*Session, error) {
	var session Session
	if err := s.db.Joins("Workspace").Joins("GitBranch").Joins("TmuxSession").First(&session, "sessions.branch_id = ?", branchID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return &session, nil
}

func (s *SessionStore) GetByTmuxSessionID(tmuxSessionID uint) (*Session, error) {
	var session Session
	if err := s.db.Joins("Workspace").Joins("GitBranch").Joins("TmuxSession").First(&session, "sessions.tmux_session_id = ?", tmuxSessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return &session, nil
}

func (s *SessionStore) OnAppStart(ctx context.Context) error {
	return nil
}

func (s *SessionStore) OnAppEnd(ctx context.Context) error {
	return nil
}

