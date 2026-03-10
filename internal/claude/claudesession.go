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
	StatusReadyForReview ClaudeSessionStatus = "ready_for_review"
	StatusCompleted      ClaudeSessionStatus = "completed"
)

type ClaudeSession struct {
	ID            string              `json:"id" gorm:"primaryKey"`
	SessionID     string              `json:"session_id" gorm:"index"`
	Status        ClaudeSessionStatus `json:"status"`
	CWD           string              `json:"cwd,omitempty"`
	LastUpdatedAt time.Time           `json:"last_updated_at"`
}
