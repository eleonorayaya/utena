package claude

import (
	"context"
	"errors"

	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

type ClaudeStore struct {
	db db.Database
}

func NewClaudeStore(database db.Database) *ClaudeStore {
	return &ClaudeStore{
		db: database,
	}
}

func (s *ClaudeStore) GetByID(id string) (*ClaudeSession, error) {
	var cs ClaudeSession
	if err := s.db.First(&cs, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrClaudeSessionNotFound
		}
		return nil, err
	}
	return &cs, nil
}

func (s *ClaudeStore) List() []ClaudeSession {
	var sessions []ClaudeSession
	s.db.Order("updated_at DESC").Find(&sessions)
	return sessions
}

func (s *ClaudeStore) ListBySessionID(sessionID string) []ClaudeSession {
	var sessions []ClaudeSession
	s.db.Where("session_id = ?", sessionID).Order("updated_at DESC").Find(&sessions)
	return sessions
}

func (s *ClaudeStore) Upsert(session *ClaudeSession) error {
	if session == nil {
		return errors.New("claude session cannot be nil")
	}
	if session.ID == "" {
		return errors.New("claude session ID cannot be empty")
	}

	return s.db.Save(session).Error
}

func (s *ClaudeStore) UpdateStatusBySessionID(sessionID string, from, to ClaudeSessionStatus) {
	s.db.Model(&ClaudeSession{}).
		Where("session_id = ? AND status = ?", sessionID, from).
		Update("status", to)
}

func (s *ClaudeStore) Delete(id string) error {
	if id == "" {
		return errors.New("claude session ID cannot be empty")
	}

	var existing ClaudeSession
	if err := s.db.First(&existing, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrClaudeSessionNotFound
		}
		return err
	}

	return s.db.Delete(&ClaudeSession{}, "id = ?", id).Error
}

func (s *ClaudeStore) OnAppStart(ctx context.Context) error {
	return nil
}

func (s *ClaudeStore) OnAppEnd(ctx context.Context) error {
	return nil
}
