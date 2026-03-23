package session

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	if err := s.db.Joins("Workspace").Preload("ClaudeSessions").First(&session, "sessions.id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return &session, nil
}

func (s *SessionStore) GetByTmuxName(tmuxName string) (*Session, error) {
	var session Session
	if err := s.db.Joins("Workspace").Preload("ClaudeSessions").First(&session, "tmux_session_name = ?", tmuxName).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return &session, nil
}

func (s *SessionStore) List() []Session {
	var sessions []Session
	s.db.Joins("Workspace").Preload("ClaudeSessions").Order("sessions.last_used_at DESC").Find(&sessions)
	return sessions
}

func (s *SessionStore) ListByWorkspace(workspaceID uint) []Session {
	var sessions []Session
	s.db.Joins("Workspace").Preload("ClaudeSessions").Where("sessions.workspace_id = ?", workspaceID).Order("sessions.last_used_at DESC").Find(&sessions)
	return sessions
}

func (s *SessionStore) Add(session *Session) error {
	if session == nil {
		return errors.New("session cannot be nil")
	}

	if err := s.db.Omit("Workspace", "ClaudeSessions").Create(session).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || isUniqueConstraintError(err) {
			return fmt.Errorf("session '%s' already exists: %w", session.TmuxSessionName, ErrSessionAlreadyExists)
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

	return s.db.Omit("Workspace", "ClaudeSessions").Save(session).Error
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

func (s *SessionStore) OnAppStart(ctx context.Context) error {
	return nil
}

func (s *SessionStore) OnAppEnd(ctx context.Context) error {
	return nil
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
