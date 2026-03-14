package claude

import (
	"errors"

	"gorm.io/gorm"
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
	gorm.Model
	ClaudeSessionID string              `json:"claude_session_id" gorm:"uniqueIndex"`
	SessionID       string              `json:"session_id" gorm:"index"`
	Status          ClaudeSessionStatus `json:"status"`
	CWD             string              `json:"cwd,omitempty"`
}
