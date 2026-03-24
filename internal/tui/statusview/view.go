package statusview

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/session"
)

func (m Model) View() string {
	if m.isExpanded() {
		return m.expandedView()
	}
	return m.collapsedView()
}

func (m Model) collapsedView() string {
	maxNameLen := m.width - 2
	if maxNameLen < 1 {
		maxNameLen = 1
	}

	var lines []string
	for _, sess := range m.orderedActiveSessions() {
		name := sessionDisplayName(sess)
		name = truncate(name, maxNameLen)
		lines = append(lines, "  "+name)
	}

	return strings.Join(lines, "\n")
}

func (m Model) expandedView() string {
	ordered := m.orderedActiveSessions()

	var parts []string
	for _, s := range ordered {
		if tab, ok := m.tabs[s.ID]; ok {
			parts = append(parts, tab.View())
		}
	}

	if len(parts) == 0 {
		empty := lipgloss.NewStyle().Foreground(colorTextMuted)
		return empty.Render("  no active sessions")
	}

	return strings.Join(parts, "\n")
}

func sessionDisplayName(s session.Session) string {
	if s.Name != "" {
		return s.Name
	}
	return s.TmuxSessionName
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return s[:maxLen]
	}
	return s[:maxLen-1] + "…"
}
