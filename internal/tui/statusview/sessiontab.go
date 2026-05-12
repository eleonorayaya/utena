package statusview

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tmux"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

type SessionTab struct {
	session     session.Session
	windows     []tmux.Window
	badge       StatusBadge
	selected    bool
	width       int
	hideWindows bool
}

func (t SessionTab) WithWidth(w int) SessionTab {
	t.width = w
	return t
}

func (t SessionTab) WithoutWindows() SessionTab {
	t.hideWindows = true
	return t
}

func (t SessionTab) WithSelected(selected bool) SessionTab {
	t.selected = selected
	t.badge, _ = t.badge.Update(statusBadgeMsg{
		ClaudeSessions: t.session.ClaudeSessions,
		Selected:       selected,
		IsAttached:     t.session.IsAttached,
	})
	return t
}

func NewSessionTab(s session.Session) SessionTab {
	return SessionTab{
		session: s,
		badge:   NewStatusBadge(s.ClaudeSessions, s.IsAttached),
	}
}

type sessionTabMsg struct {
	Session  session.Session
	Windows  []tmux.Window
	Selected bool
	Width    int
}

func (t SessionTab) Update(msg tea.Msg) (SessionTab, tea.Cmd) {
	switch msg := msg.(type) {
	case sessionTabMsg:
		t.session = msg.Session
		t.windows = msg.Windows
		t.selected = msg.Selected
		t.width = msg.Width
		t.badge, _ = t.badge.Update(statusBadgeMsg{
			ClaudeSessions: msg.Session.ClaudeSessions,
			Selected:       msg.Selected,
			IsAttached:     msg.Session.IsAttached,
		})
	}
	return t, nil
}

func (t SessionTab) View() string {
	if t.width < 1 {
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

func (t SessionTab) bg() lipgloss.TerminalColor {
	if t.selected {
		return theme.Current.Selection
	}
	if t.session.IsAttached {
		return theme.Current.SurfaceActive
	}
	return lipgloss.NoColor{}
}

func (t SessionTab) accentColor() lipgloss.Color {
	switch claude.AggregateStatus(t.session.ClaudeSessions) {
	case claude.StatusNeedsAttention:
		return theme.Current.Primary
	case claude.StatusWorking:
		return theme.Current.AccentLavender
	case claude.StatusReadyForReview:
		return theme.Current.AccentMint
	default:
		return theme.Current.SurfaceVariant
	}
}

func (t SessionTab) accent(bg lipgloss.TerminalColor) string {
	barBase := lipgloss.NewStyle().Foreground(t.accentColor())
	return barBase.Background(bg).Render("▐") + lipgloss.NewStyle().Background(bg).Render(" ")
}

func (t SessionTab) renderHeader(bg lipgloss.TerminalColor) []string {
	s := t.session

	nStyle := lipgloss.NewStyle().Foreground(theme.Current.Text)
	if s.IsAttached {
		nStyle = lipgloss.NewStyle().Foreground(theme.Current.TextEmphasis).Bold(true)
	}
	nStyle = nStyle.Background(bg)

	badgeStr := t.badge.View()
	accent := t.accent(bg)
	accentWidth := 2

	maxName := t.width - accentWidth - lipgloss.Width(badgeStr)
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
		pad := t.width - usedWidth
		if pad < 1 {
			pad = 1
		}
		padStr := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", pad))
		nameLine = accent + nameStr + padStr + badgeStr
	}
	nameLine = t.padLine(nameLine, bg)

	var result []string
	result = append(result, nameLine)

	wsName := s.WorkspaceDisplay()
	if wsName != "" {
		wsStyle := lipgloss.NewStyle().Foreground(theme.Current.Tertiary).Background(bg)
		timeStr := ""
		if !s.LastUsedAt.IsZero() {
			tStyle := lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Background(bg)
			timeStr = tStyle.Render(" · " + common.TimeAgo(s.LastUsedAt))
		}
		wsLine := accent + wsStyle.Render(wsName) + timeStr
		wsLine = t.padLine(wsLine, bg)
		result = append(result, wsLine)
	} else if t.hideWindows {
		result = append(result, t.padLine(accent, bg))
	}

	return result
}

func (t SessionTab) renderWindows(bg lipgloss.TerminalColor) []string {
	if t.hideWindows || len(t.windows) == 0 {
		return nil
	}

	accent := t.accent(bg)

	var lines []string
	for _, w := range t.windows {
		marker := "  "
		wStyle := lipgloss.NewStyle().Foreground(theme.Current.TextMuted)
		if w.Active {
			marker = "› "
			wStyle = lipgloss.NewStyle().Foreground(theme.Current.AccentBlue)
		}
		wStyle = wStyle.Background(bg)
		line := accent + wStyle.Render(marker+w.Name)
		line = t.padLine(line, bg)
		lines = append(lines, line)
	}
	return lines
}

func (t SessionTab) padLine(line string, bg lipgloss.TerminalColor) string {
	lineWidth := lipgloss.Width(line)
	if lineWidth < t.width {
		line += lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", t.width-lineWidth))
	}
	return line
}

func (t SessionTab) emptyLine(bg lipgloss.TerminalColor) string {
	return lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", t.width))
}
