package statusview

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/claude"
)

var (
	selectedBg = lipgloss.Color("#ffd5d5")

	activeSessionBg   = lipgloss.Color("#f0e8e0")
	inactiveSessionBg = lipgloss.Color("#e8dfd5")

	accentAttentionColor = lipgloss.Color("#e6537a")
	accentWorkingColor   = lipgloss.Color("#6a9bc3")
	accentReviewColor    = lipgloss.Color("#7bb88e")
	accentIdleColor      = lipgloss.Color("#d8cfc5")

	workspaceStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9370b9"))

	sessionNameStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#4a4a4a"))

	sessionAttachedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#2a2a2a")).
				Bold(true)

	windowActiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6a9bc3"))

	windowDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8a7873"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8a7873"))

	attentionBadgeStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#fef6f0")).
				Background(lipgloss.Color("#d16577"))

	workingBadgeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#d97c6e"))

	reviewBadgeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#5fafa5"))
)

func accentBarColor(claudeSessions []claude.ClaudeSession) lipgloss.Color {
	switch claude.AggregateStatus(claudeSessions) {
	case claude.StatusNeedsAttention:
		return accentAttentionColor
	case claude.StatusWorking:
		return accentWorkingColor
	case claude.StatusReadyForReview:
		return accentReviewColor
	default:
		return accentIdleColor
	}
}

func claudeBadgeParts(sessions []claude.ClaudeSession) (*lipgloss.Style, string) {
	switch claude.AggregateStatus(sessions) {
	case claude.StatusNeedsAttention:
		s := attentionBadgeStyle
		return &s, " ! "
	case claude.StatusWorking:
		s := workingBadgeStyle
		return &s, " ~ "
	case claude.StatusReadyForReview:
		s := reviewBadgeStyle
		return &s, " ✓ "
	case claude.StatusDone:
		s := dimStyle
		return &s, " ✓ "
	default:
		return nil, ""
	}
}
