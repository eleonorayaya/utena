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
	if len(s.Workspaces) > 1 {
		var names []string
		for _, sw := range s.Workspaces {
			if sw.Workspace != nil && sw.Workspace.Name != "" {
				names = append(names, sw.Workspace.Name)
			}
		}
		switch len(names) {
		case 0:
			return fmt.Sprintf("%d workspaces", len(s.Workspaces))
		case 1, 2, 3:
			return fmt.Sprintf("%d workspaces · %s", len(names), strings.Join(names, ", "))
		default:
			return fmt.Sprintf("%d workspaces · %s, …", len(names), strings.Join(names[:3], ", "))
		}
	}
	if s.Workspace != nil && s.Workspace.Name != "" {
		return s.Workspace.Name
	}
	if len(s.Workspaces) > 0 && s.Workspaces[0].Workspace != nil {
		return s.Workspaces[0].Workspace.Name
	}
	return "no workspace"
}

func (i sessionItem) FilterValue() string { return i.displayName() }

func aggregateClaudeStatus(sessions []claude.ClaudeSession) claude.ClaudeSessionStatus {
	return claude.AggregateStatus(sessions)
}
