package statusview

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/tui/theme"
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
		return theme.Current.Selection
	}
	if b.isAttached {
		return theme.Current.SurfaceActive
	}
	return lipgloss.NoColor{}
}

func (b StatusBadge) View() string {
	style, text := b.parts()
	if style == nil {
		return ""
	}
	rowBg := b.bg()
	if _, isNoColor := rowBg.(lipgloss.NoColor); !isNoColor {
		*style = (*style).Background(rowBg)
	}
	return (*style).Render(text)
}

func (b StatusBadge) Width() int {
	return lipgloss.Width(b.View())
}

func (b StatusBadge) WithSelected(selected bool) StatusBadge {
	b.selected = selected
	return b
}

func (b StatusBadge) parts() (*lipgloss.Style, string) {
	switch b.claudeStatus {
	case claude.StatusNeedsAttention:
		s := lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.Current.TextOnPrimary).
			Background(theme.Current.PrimaryVariant)
		return &s, " ! "
	case claude.StatusWorking:
		s := lipgloss.NewStyle().
			Foreground(theme.Current.TextOnPrimary).
			Background(theme.Current.AccentLavender)
		return &s, " ~ "
	case claude.StatusReadyForReview:
		s := lipgloss.NewStyle().
			Foreground(theme.Current.TextOnPrimary).
			Background(theme.Current.AccentMint)
		return &s, " ✓ "
	case claude.StatusDone:
		s := lipgloss.NewStyle().
			Foreground(theme.Current.TextMuted).
			Background(theme.Current.SurfaceVariant)
		return &s, " ✓ "
	default:
		return nil, ""
	}
}
