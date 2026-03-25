package claude

import (
	"github.com/eleonorayaya/utena/internal/common"
	"gorm.io/gorm"
)

var ErrClaudeSessionNotFound = common.NewNotFound("claude session not found")

type ClaudeSessionStatus string

const (
	StatusIdle           ClaudeSessionStatus = "idle"
	StatusWorking        ClaudeSessionStatus = "working"
	StatusNeedsAttention ClaudeSessionStatus = "needs_attention"
	StatusReadyForReview ClaudeSessionStatus = "ready_for_review"
	StatusDone           ClaudeSessionStatus = "done"
)

type ClaudeSession struct {
	gorm.Model
	ClaudeSessionID string              `json:"claude_session_id" gorm:"uniqueIndex"`
	SessionID       uint                `json:"session_id" gorm:"index"`
	Status          ClaudeSessionStatus `json:"status"`
	CWD             string              `json:"cwd,omitempty"`
}

func AggregateStatus(sessions []ClaudeSession) ClaudeSessionStatus {
	if len(sessions) == 0 {
		return ""
	}
	hasNeedsAttention := false
	hasWorking := false
	hasReadyForReview := false
	hasDone := false
	hasIdle := false
	for _, cs := range sessions {
		switch cs.Status {
		case StatusNeedsAttention:
			hasNeedsAttention = true
		case StatusWorking:
			hasWorking = true
		case StatusReadyForReview:
			hasReadyForReview = true
		case StatusDone:
			hasDone = true
		case StatusIdle:
			hasIdle = true
		}
	}
	if hasNeedsAttention {
		return StatusNeedsAttention
	}
	if hasWorking {
		return StatusWorking
	}
	if hasReadyForReview {
		return StatusReadyForReview
	}
	if hasDone {
		return StatusDone
	}
	if hasIdle {
		return StatusIdle
	}
	return ""
}
