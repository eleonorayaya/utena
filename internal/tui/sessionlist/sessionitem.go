package sessionlist

import (
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/session"
)

type sessionItem struct {
	session      session.Session
	claudeStatus claude.ClaudeSessionStatus
}

func (i sessionItem) displayName() string {
	if i.session.Name != "" {
		return i.session.Name
	}
	if i.session.TmuxSession != nil {
		return i.session.TmuxSession.Name
	}
	return ""
}

func (i sessionItem) Title() string {
	title := i.displayName()
	switch i.session.Status {
	case session.StatusCreating:
		title += " (creating...)"
	case session.StatusBroken:
		title += " (broken)"
	default:
		if i.session.IsAttached {
			title += " (attached)"
		}
	}
	switch i.claudeStatus {
	case claude.StatusNeedsAttention:
		title += " [needs attention]"
	case claude.StatusWorking:
		title += " [working]"
	case claude.StatusReadyForReview:
		title += " [ready for review]"
	case claude.StatusDone:
		title += " [done]"
	case claude.StatusIdle:
		title += " [idle]"
	}
	return title
}

func (i sessionItem) Description() string {
	var name string
	if i.session.Workspace != nil {
		name = i.session.Workspace.Name
	}
	if name == "" {
		name = "no workspace"
	}
	if !i.session.LastUsedAt.IsZero() {
		return name + " · " + common.TimeAgo(i.session.LastUsedAt)
	}
	return name
}

func (i sessionItem) FilterValue() string { return i.displayName() }

func aggregateClaudeStatus(sessions []claude.ClaudeSession) claude.ClaudeSessionStatus {
	return claude.AggregateStatus(sessions)
}
