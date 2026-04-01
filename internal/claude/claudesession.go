package claude

import (
	"errors"
	"fmt"

	"github.com/eleonorayaya/utena/internal/common"
	"gorm.io/gorm"
)

var ErrClaudeSessionNotFound = errors.New("claude session not found")

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

func (cs *ClaudeSession) Signals() []common.Signal {
	var severity common.SignalSeverity
	var label string
	switch cs.Status {
	case StatusNeedsAttention:
		severity = common.SeverityUrgent
		label = "needs attention"
	case StatusWorking:
		severity = common.SeverityActive
		label = "working"
	case StatusReadyForReview:
		severity = common.SeverityWarning
		label = "ready for review"
	case StatusDone:
		severity = common.SeveritySuccess
		label = "done"
	case StatusIdle:
		severity = common.SeverityInfo
		label = "idle"
	default:
		return nil
	}
	return []common.Signal{{
		Source:   "claude",
		Key:      fmt.Sprintf("claude:%d", cs.ID),
		Severity: severity,
		Label:    label,
	}}
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
