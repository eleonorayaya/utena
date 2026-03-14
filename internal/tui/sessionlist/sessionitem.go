package sessionlist

import (
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/session"
)

type sessionItem struct {
	session      session.Session
	claudeStatus string
}

func (i sessionItem) displayName() string {
	if i.session.Name != "" {
		return i.session.Name
	}
	return i.session.TmuxSessionName
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
	if i.claudeStatus != "" {
		title += " " + i.claudeStatus
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

func aggregateClaudeStatus(sessions []claude.ClaudeSession) string {
	if len(sessions) == 0 {
		return ""
	}

	hasNeedsAttention := false
	hasReadyForReview := false
	hasWorking := false
	for _, cs := range sessions {
		switch cs.Status {
		case claude.StatusNeedsAttention:
			hasNeedsAttention = true
		case claude.StatusReadyForReview:
			hasReadyForReview = true
		case claude.StatusWorking:
			hasWorking = true
		}
	}

	if hasNeedsAttention {
		return "[needs attention]"
	}
	if hasWorking {
		return "[working]"
	}
	if hasReadyForReview {
		return "[ready for review]"
	}
	return "[done]"
}
