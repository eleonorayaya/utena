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
	badge    ClaudeBadge
}

func NewSessionTab(s session.Session) SessionTab {
	return SessionTab{
		Session: s,
		badge:   NewClaudeBadge(s.ClaudeSessions),
	}
}

func (t SessionTab) Update(s session.Session, windows []tmux.Window, selected bool, width int) SessionTab {
	t.Session = s
	t.Windows = windows
	t.Selected = selected
	t.Width = width
	t.badge = NewClaudeBadge(s.ClaudeSessions)
	return t
}

func (t SessionTab) View() string {
	if t.Width < 1 {
		return ""
	}
	bg := t.bg()
	var lines []string
	lines = append(lines, t.emptyLine(bg))
	lines = append(lines, t.renderHeader(bg)...)
	lines = append(lines, t.renderWindows(bg)...)
	lines = append(lines, t.emptyLine(bg))
	return strings.Join(lines, "\n")
}

func (t SessionTab) bg() lipgloss.Color {
	if t.Selected {
		return colorSelection
	}
	if t.Session.IsAttached {
		return colorSurfaceActive
	}
	return colorSurface
}

func (t SessionTab) accent(bg lipgloss.Color) string {
	barBase := lipgloss.NewStyle().Foreground(t.badge.AccentColor())
	return barBase.Background(bg).Render("▐") + lipgloss.NewStyle().Background(bg).Render(" ")
}

func (t SessionTab) renderHeader(bg lipgloss.Color) []string {
	s := t.Session

	nStyle := lipgloss.NewStyle().Foreground(colorText)
	if s.IsAttached {
		nStyle = lipgloss.NewStyle().Foreground(colorTextEmphasis).Bold(true)
	}
	nStyle = nStyle.Background(bg)

	badgeStr := t.badge.Render(bg)
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
	nameLine = t.padLine(nameLine, bg)

	var result []string
	result = append(result, nameLine)

	wsName := ""
	if s.Workspace != nil {
		wsName = s.Workspace.Name
	}
	if wsName != "" {
		wsStyle := lipgloss.NewStyle().Foreground(colorTertiary).Background(bg)
		timeStr := ""
		if !s.LastUsedAt.IsZero() {
			tStyle := lipgloss.NewStyle().Foreground(colorTextMuted).Background(bg)
			timeStr = tStyle.Render(" · " + common.TimeAgo(s.LastUsedAt))
		}
		wsLine := accent + wsStyle.Render(wsName) + timeStr
		wsLine = t.padLine(wsLine, bg)
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
		wStyle := lipgloss.NewStyle().Foreground(colorTextMuted)
		if w.Active {
			marker = "› "
			wStyle = lipgloss.NewStyle().Foreground(colorAccentBlue)
		}
		wStyle = wStyle.Background(bg)
		line := accent + wStyle.Render(marker+w.Name)
		line = t.padLine(line, bg)
		lines = append(lines, line)
	}
	return lines
}

func (t SessionTab) padLine(line string, bg lipgloss.Color) string {
	lineWidth := lipgloss.Width(line)
	if lineWidth < t.Width {
		line += lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", t.Width-lineWidth))
	}
	return line
}

func (t SessionTab) emptyLine(bg lipgloss.Color) string {
	return lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", t.Width))
}
