package session

import (
	"errors"
	"time"
)

var ErrSessionAlreadyExists = errors.New("session already exists")
var ErrSessionNotFound = errors.New("session not found")

type Session struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	IsAttached  bool      `json:"is_attached"`
	IsActive    bool      `json:"is_active"`
	IsDead      bool      `json:"is_dead"`
	LastUsedAt  time.Time `json:"last_used_at"`
}
