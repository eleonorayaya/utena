package statusview

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/common"
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
	innerWidth := m.width
	if innerWidth < 1 {
		innerWidth = 1
	}

	ordered := m.orderedActiveSessions()

	var lines []string
	for i, s := range ordered {
		selected := i == m.cursor && m.focused
		bg := sessionBg(s, selected)
		lines = append(lines, emptyLine(innerWidth, bg))
		lines = append(lines, m.renderSessionBlock(s, bg, innerWidth)...)
		lines = append(lines, m.renderWindows(s, innerWidth, bg)...)
		lines = append(lines, emptyLine(innerWidth, bg))
	}

	if len(ordered) == 0 {
		lines = append(lines, dimStyle.Render("  no active sessions"))
	}

	return strings.Join(lines, "\n")
}

func sessionBg(s session.Session, selected bool) lipgloss.Color {
	if selected {
		return selectedBg
	}
	if s.IsAttached {
		return activeSessionBg
	}
	return inactiveSessionBg
}

func (m Model) renderSessionBlock(s session.Session, bg lipgloss.Color, innerWidth int) []string {
	nStyle := sessionNameStyle
	if s.IsAttached {
		nStyle = sessionAttachedStyle
	}
	wsStyle := workspaceStyle
	bStyle, badgeText := claudeBadgeParts(s.ClaudeSessions)

	nStyle = nStyle.Background(bg)
	wsStyle = wsStyle.Background(bg)
	if bStyle != nil {
		styled := (*bStyle).Background(bg)
		bStyle = &styled
	}

	badgeStr := ""
	if bStyle != nil {
		badgeStr = (*bStyle).Render(badgeText)
	}

	barColor := accentBarColor(s.ClaudeSessions)
	barBase := lipgloss.NewStyle().Foreground(barColor)
	accent := renderAccent(barBase, bg)
	accentWidth := 2

	maxName := innerWidth - accentWidth - lipgloss.Width(badgeStr)
	if badgeStr != "" {
		maxName--
	}
	if maxName < 4 {
		maxName = 4
	}

	name := sessionDisplayName(s)
	truncName := truncate(name, maxName)
	nameStr := nStyle.Render(truncName)

	nameLine := accent + nameStr
	if badgeStr != "" {
		usedWidth := accentWidth + lipgloss.Width(truncName) + lipgloss.Width(badgeStr)
		pad := innerWidth - usedWidth
		if pad < 1 {
			pad = 1
		}
		padStr := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", pad))
		nameLine = accent + nameStr + padStr + badgeStr
	}
	nameLine = padLine(nameLine, innerWidth, bg)

	var result []string
	result = append(result, nameLine)

	wsName := ""
	if s.Workspace != nil {
		wsName = s.Workspace.Name
	}
	if wsName != "" {
		timeStr := ""
		if !s.LastUsedAt.IsZero() {
			tStyle := dimStyle.Background(bg)
			timeStr = tStyle.Render(" · " + common.TimeAgo(s.LastUsedAt))
		}
		wsLine := accent + wsStyle.Render(wsName) + timeStr
		wsLine = padLine(wsLine, innerWidth, bg)
		result = append(result, wsLine)
	}

	return result
}

func (m Model) renderWindows(s session.Session, innerWidth int, bg lipgloss.Color) []string {
	windows := m.windowsBySession[s.TmuxSessionName]
	if len(windows) == 0 {
		return nil
	}

	barColor := accentBarColor(s.ClaudeSessions)
	barBase := lipgloss.NewStyle().Foreground(barColor)
	accent := renderAccent(barBase, bg)

	var lines []string
	for _, w := range windows {
		marker := "  "
		wStyle := windowDimStyle
		if w.Active {
			marker = "› "
			wStyle = windowActiveStyle
		}
		wStyle = wStyle.Background(bg)
		line := accent + wStyle.Render(marker+w.Name)
		line = padLine(line, innerWidth, bg)
		lines = append(lines, line)
	}
	return lines
}

func padLine(line string, width int, bg lipgloss.Color) string {
	lineWidth := lipgloss.Width(line)
	if lineWidth < width {
		line += lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", width-lineWidth))
	}
	return line
}

func emptyLine(width int, bg lipgloss.Color) string {
	return lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", width))
}

func renderAccent(barBase lipgloss.Style, bg lipgloss.Color) string {
	return barBase.Background(bg).Render("▐") + lipgloss.NewStyle().Background(bg).Render(" ")
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
