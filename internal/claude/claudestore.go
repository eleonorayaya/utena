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
	if session.ClaudeSessionID == "" {
		return errors.New("claude session ID cannot be empty")
	}

	var existing ClaudeSession
	if err := s.db.First(&existing, "claude_session_id = ?", session.ClaudeSessionID).Error; err == nil {
		existing.SessionID = session.SessionID
		existing.Status = session.Status
		existing.CWD = session.CWD
		return s.db.Save(&existing).Error
	}

	return s.db.Create(session).Error
}

func (s *ClaudeStore) UpdateStatusBySessionID(sessionID string, from, to ClaudeSessionStatus) {
	s.db.Model(&ClaudeSession{}).
		Where("session_id = ? AND status = ?", sessionID, from).
		Update("status", to)
}

func (s *ClaudeStore) UpdateStatusByClaudeSessionID(claudeSessionID string, status ClaudeSessionStatus) error {
	return s.db.Model(&ClaudeSession{}).
		Where("claude_session_id = ?", claudeSessionID).
		Update("status", status).Error
}

func (s *ClaudeStore) DeleteByClaudeSessionID(claudeSessionID string) error {
	if claudeSessionID == "" {
		return errors.New("claude session ID cannot be empty")
	}

	var existing ClaudeSession
	if err := s.db.First(&existing, "claude_session_id = ?", claudeSessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrClaudeSessionNotFound
		}
		return err
	}

	return s.db.Delete(&ClaudeSession{}, "id = ?", existing.ID).Error
}

func (s *ClaudeStore) OnAppStart(ctx context.Context) error {
	return nil
}

func (s *ClaudeStore) OnAppEnd(ctx context.Context) error {
	return nil
}
