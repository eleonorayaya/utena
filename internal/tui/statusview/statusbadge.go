package statusview

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/claude"
)

type StatusBadge struct {
	claudeStatus claude.ClaudeSessionStatus
	selected     bool
	isAttached   bool
}

func NewStatusBadge(sessions []claude.ClaudeSession, isAttached bool) StatusBadge {
	return StatusBadge{
		claudeStatus: claude.AggregateStatus(sessions),
		isAttached:   isAttached,
	}
}

type statusBadgeMsg struct {
	ClaudeSessions []claude.ClaudeSession
	Selected       bool
	IsAttached     bool
}

func (b StatusBadge) Update(msg tea.Msg) (StatusBadge, tea.Cmd) {
	switch msg := msg.(type) {
	case statusBadgeMsg:
		b.claudeStatus = claude.AggregateStatus(msg.ClaudeSessions)
		b.selected = msg.Selected
		b.isAttached = msg.IsAttached
	}
	return b, nil
}

func (b StatusBadge) bg() lipgloss.TerminalColor {
	if b.selected {
		return colorSelection
	}
	if b.isAttached {
		return colorSurfaceActive
	}
	return lipgloss.NoColor{}
}

func (b StatusBadge) AccentColor() lipgloss.Color {
	switch b.claudeStatus {
	case claude.StatusNeedsAttention:
		return colorPrimary
	case claude.StatusWorking:
		return colorAccentBlue
	case claude.StatusReadyForReview:
		return colorAccentMint
	default:
		return colorSurfaceVariant
	}
}

func (b StatusBadge) View() string {
	style, text := b.parts()
	if style == nil {
		return ""
	}
	return (*style).Background(b.bg()).Render(text)
}

func (b StatusBadge) Width() int {
	return lipgloss.Width(b.View())
}

func (b StatusBadge) parts() (*lipgloss.Style, string) {
	switch b.claudeStatus {
	case claude.StatusNeedsAttention:
		s := lipgloss.NewStyle().
			Bold(true).
			Foreground(colorTextOnPrimary).
			Background(colorPrimaryVariant)
		return &s, " ! "
	case claude.StatusWorking:
		s := lipgloss.NewStyle().Foreground(colorSecondary)
		return &s, " ~ "
	case claude.StatusReadyForReview:
		s := lipgloss.NewStyle().Foreground(colorAccentMint)
		return &s, " ✓ "
	case claude.StatusDone:
		s := lipgloss.NewStyle().Foreground(colorTextMuted)
		return &s, " ✓ "
	default:
		return nil, ""
	}
}
