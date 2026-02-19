package claude

import (
	"errors"
	"time"
)

var ErrClaudeSessionNotFound = errors.New("claude session not found")

type ClaudeSessionStatus string

const (
	StatusWorking        ClaudeSessionStatus = "working"
	StatusNeedsAttention ClaudeSessionStatus = "needs_attention"
	StatusCompleted      ClaudeSessionStatus = "completed"
)

type ClaudeSession struct {
	ID            string              `json:"id"`
	SessionID     string              `json:"session_id"`
	Status        ClaudeSessionStatus `json:"status"`
	CWD           string              `json:"cwd,omitempty"`
	LastUpdatedAt time.Time           `json:"last_updated_at"`
}
