package claude

import (
	"context"

	"github.com/eleonorayaya/utena/internal/db"
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

func (s *ClaudeStore) ListBySessionID(sessionID uint) []ClaudeSession {
	var sessions []ClaudeSession
	s.db.Where("session_id = ?", sessionID).Order("updated_at DESC").Find(&sessions)
	return sessions
}

func (s *ClaudeStore) Create(cs *ClaudeSession) error {
	return s.db.Create(cs).Error
}

func (s *ClaudeStore) UpdateStatus(claudeSessionID string, status ClaudeSessionStatus) error {
	return s.db.Model(&ClaudeSession{}).
		Where("claude_session_id = ?", claudeSessionID).
		Update("status", status).Error
}

func (s *ClaudeStore) OnAppStart(ctx context.Context) error {
	s.db.Exec("DROP TABLE IF EXISTS claude_sessions")
	return s.db.Migrate(&ClaudeSession{})
}

func (s *ClaudeStore) OnAppEnd(ctx context.Context) error {
	return nil
}
