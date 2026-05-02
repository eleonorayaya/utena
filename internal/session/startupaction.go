package session

import (
	"encoding/json"

	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

type StartupActionType string

const StartupActionTypeClaude StartupActionType = "claude"

type StartupAction struct {
	gorm.Model
	SessionID uint              `gorm:"index;not null"`
	Type      StartupActionType `gorm:"not null"`
	Options   string            `gorm:"type:text"`
}

type ClaudeActionOptions struct {
	Prompt string `json:"prompt"`
}

func marshalOptions(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

type StartupActionStore struct {
	db db.Database
}

func NewStartupActionStore(database db.Database) *StartupActionStore {
	return &StartupActionStore{db: database}
}

func (s *StartupActionStore) Add(action *StartupAction) error {
	return s.db.Create(action).Error
}

func (s *StartupActionStore) ListBySessionID(sessionID uint) ([]StartupAction, error) {
	var actions []StartupAction
	if err := s.db.Where("session_id = ?", sessionID).Find(&actions).Error; err != nil {
		return nil, err
	}
	return actions, nil
}
