package statusview

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tmux"
)

type SessionTab struct {
	Session  session.Session
	Windows  []tmux.Window
	Selected bool
	Width    int
}

func NewSessionTab(s session.Session) SessionTab {
	return SessionTab{Session: s}
}

func (t SessionTab) Update(s session.Session, windows []tmux.Window, selected bool, width int) SessionTab {
	t.Session = s
	t.Windows = windows
	t.Selected = selected
	t.Width = width
	return t
}

func (t SessionTab) View() string {
	if t.Width < 1 {
		return ""
	}
	bg := t.bg()
	var lines []string
	lines = append(lines, emptyLine(t.Width, bg))
	lines = append(lines, t.renderHeader(bg)...)
	lines = append(lines, t.renderWindows(bg)...)
	lines = append(lines, emptyLine(t.Width, bg))
	return strings.Join(lines, "\n")
}

func (t SessionTab) bg() lipgloss.Color {
	if t.Selected {
		return selectedBg
	}
	if t.Session.IsAttached {
		return activeSessionBg
	}
	return inactiveSessionBg
}

func (t SessionTab) accent(bg lipgloss.Color) string {
	barColor := accentBarColor(t.Session.ClaudeSessions)
	barBase := lipgloss.NewStyle().Foreground(barColor)
	return renderAccent(barBase, bg)
}

func (t SessionTab) renderHeader(bg lipgloss.Color) []string {
	s := t.Session
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

	accent := t.accent(bg)
	accentWidth := 2

	maxName := t.Width - accentWidth - lipgloss.Width(badgeStr)
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
		pad := t.Width - usedWidth
		if pad < 1 {
			pad = 1
		}
		padStr := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", pad))
		nameLine = accent + nameStr + padStr + badgeStr
	}
	nameLine = padLine(nameLine, t.Width, bg)

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
		wsLine = padLine(wsLine, t.Width, bg)
		result = append(result, wsLine)
	}

	return result
}

func (t SessionTab) renderWindows(bg lipgloss.Color) []string {
	if len(t.Windows) == 0 {
		return nil
	}

	accent := t.accent(bg)

	var lines []string
	for _, w := range t.Windows {
		marker := "  "
		wStyle := windowDimStyle
		if w.Active {
			marker = "› "
			wStyle = windowActiveStyle
		}
		wStyle = wStyle.Background(bg)
		line := accent + wStyle.Render(marker+w.Name)
		line = padLine(line, t.Width, bg)
		lines = append(lines, line)
	}
	return lines
}

