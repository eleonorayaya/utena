package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

func fg(c lipgloss.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(c)
}

func ClaudeGlyph(st claude.ClaudeSessionStatus) Span {
	switch st {
	case claude.StatusNeedsAttention:
		return Span{Text: "!", Style: fg(theme.Current.StatusAttention).Bold(true)}
	case claude.StatusReadyForReview:
		return Span{Text: "✓", Style: fg(theme.Current.StatusReview).Bold(true)}
	case claude.StatusWorking:
		return Span{Text: "~", Style: fg(theme.Current.StatusWorking)}
	case claude.StatusDone:
		return Span{Text: "✓", Style: fg(theme.Current.StatusDone)}
	case claude.StatusIdle:
		return Span{Text: "·", Style: fg(theme.Current.StatusIdle)}
	default:
		return Span{}
	}
}

func ClaudeStatusText(st claude.ClaudeSessionStatus) string {
	switch st {
	case claude.StatusNeedsAttention:
		return "Needs your input"
	case claude.StatusReadyForReview:
		return "Ready for review"
	case claude.StatusWorking:
		return "Working"
	case claude.StatusIdle:
		return "Idle"
	case claude.StatusDone:
		return "Done"
	default:
		return ""
	}
}

func PRBadge(state git.PRState) string {
	switch state {
	case git.PRStateOpen:
		return fg(theme.Current.StatusReady).Render("● open")
	case git.PRStateDraft:
		return fg(theme.Current.TextMuted).Render("◌ draft")
	case git.PRStateMerged:
		return fg(theme.Current.Tertiary).Render("⊕ merged")
	case git.PRStateClosed:
		return fg(theme.Current.Error).Render("✕ closed")
	default:
		return fg(theme.Current.TextMuted).Render(string(state))
	}
}

func PRCounts(prs []git.PullRequest) (ready, draft int) {
	for _, pr := range prs {
		switch pr.State {
		case git.PRStateOpen:
			ready++
		case git.PRStateDraft:
			draft++
		}
	}
	return ready, draft
}

func PRCountSpans(ready, draft int) []Span {
	var spans []Span
	if ready > 0 {
		spans = append(spans, Span{Text: fmt.Sprintf("●%d", ready), Style: fg(theme.Current.StatusReady)})
	}
	if draft > 0 {
		if len(spans) > 0 {
			spans = append(spans, Span{Text: " "})
		}
		spans = append(spans, Span{Text: fmt.Sprintf("◌%d", draft), Style: fg(theme.Current.TextMuted)})
	}
	return spans
}

func SectionHeader(title string, count int) string {
	return lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true).Render(title) +
		fg(theme.Current.TextMuted).Render(fmt.Sprintf(" %d", count))
}
