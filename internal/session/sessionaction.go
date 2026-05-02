package session

import (
	"encoding/json"

	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

type SessionActionTrigger string

const TriggerOnCreate SessionActionTrigger = "on_create"

type SessionActionType string

const SessionActionTypeClaude SessionActionType = "claude"

type SessionAction struct {
	gorm.Model
	SessionID uint                 `gorm:"index;not null"`
	Trigger   SessionActionTrigger `gorm:"not null"`
	Type      SessionActionType    `gorm:"not null"`
	Options   string               `gorm:"type:text"`
	Error     string               `gorm:"type:text"`
}

type ClaudeActionOptions struct {
	Prompt string `json:"prompt"`
}

func marshalOptions(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

type SessionActionStore struct {
	db db.Database
}

func NewSessionActionStore(database db.Database) *SessionActionStore {
	return &SessionActionStore{db: database}
}

func (s *SessionActionStore) Add(action *SessionAction) error {
	return s.db.Create(action).Error
}

func (s *SessionActionStore) ListBySessionIDAndTrigger(sessionID uint, trigger SessionActionTrigger) ([]SessionAction, error) {
	var actions []SessionAction
	if err := s.db.Where("session_id = ? AND trigger = ?", sessionID, trigger).Find(&actions).Error; err != nil {
		return nil, err
	}
	return actions, nil
}

func (s *SessionActionStore) UpdateError(action *SessionAction, errMsg string) error {
	action.Error = errMsg
	return s.db.Model(action).Update("error", errMsg).Error
}
