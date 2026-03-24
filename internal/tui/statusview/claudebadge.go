package statusview

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/claude"
)

type ClaudeBadge struct {
	status claude.ClaudeSessionStatus
}

func NewClaudeBadge() ClaudeBadge {
	return ClaudeBadge{}
}

type claudeSessionsMsg struct {
	Sessions []claude.ClaudeSession
}

func (b ClaudeBadge) Update(msg claudeSessionsMsg) ClaudeBadge {
	b.status = claude.AggregateStatus(msg.Sessions)
	return b
}

func (b ClaudeBadge) AccentColor() lipgloss.Color {
	switch b.status {
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

func (b ClaudeBadge) Render(bg lipgloss.Color) string {
	style, text := b.parts()
	if style == nil {
		return ""
	}
	return (*style).Background(bg).Render(text)
}

func (b ClaudeBadge) Width() int {
	return lipgloss.Width(b.Render(colorSurface))
}

func (b ClaudeBadge) parts() (*lipgloss.Style, string) {
	switch b.status {
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
