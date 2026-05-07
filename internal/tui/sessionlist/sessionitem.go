package sessionlist

import (
	"fmt"
	"strings"

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
	if i.session.Status != session.StatusBroken && i.session.StatusError != "" {
		title += " [!]"
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
	name := workspaceLabel(i.session)
	if !i.session.LastUsedAt.IsZero() {
		return name + " · " + common.TimeAgo(i.session.LastUsedAt)
	}
	return name
}

func workspaceLabel(s session.Session) string {
	names := s.WorkspaceNames()
	switch len(names) {
	case 0:
		return "no workspace"
	case 1:
		return names[0]
	case 2, 3:
		return fmt.Sprintf("%d workspaces · %s", len(names), strings.Join(names, ", "))
	default:
		return fmt.Sprintf("%d workspaces · %s, …", len(names), strings.Join(names[:3], ", "))
	}
}

func (i sessionItem) FilterValue() string { return i.displayName() }

func aggregateClaudeStatus(sessions []claude.ClaudeSession) claude.ClaudeSessionStatus {
	return claude.AggregateStatus(sessions)
}
