package sessionlist

import (
	"fmt"
	"time"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
)

type sessionItem struct {
	session      session.Session
	claudeStatus string
}

func (i sessionItem) Title() string {
	title := i.session.ID
	if i.session.IsAttached {
		title += " (attached)"
	}
	if i.claudeStatus != "" {
		title += " " + i.claudeStatus
	}
	return title
}

func (i sessionItem) Description() string {
	name := i.session.WorkspaceName
	if name == "" {
		name = "no workspace"
	}
	if !i.session.LastUsedAt.IsZero() {
		return name + " · " + timeAgo(i.session.LastUsedAt)
	}
	return name
}

func (i sessionItem) FilterValue() string { return i.session.ID }

func aggregateClaudeStatus(sessions []claude.ClaudeSession) string {
	if len(sessions) == 0 {
		return ""
	}

	hasNeedsAttention := false
	hasWorking := false
	for _, cs := range sessions {
		switch cs.Status {
		case claude.StatusNeedsAttention:
			hasNeedsAttention = true
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
	return "[done]"
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "yesterday"
		}
		if days < 30 {
			return fmt.Sprintf("%d days ago", days)
		}
		return t.Format("Jan 2")
	}
}
