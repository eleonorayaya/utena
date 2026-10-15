package sessionlist

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

const (
	gutterColW   = 2
	glyphColW    = 2
	attachedColW = 2
	prColW       = 7
	timeColW     = 4
)

func nameStyle(s session.Session, selected bool) lipgloss.Style {
	switch s.Status {
	case session.StatusBroken, session.StatusCreating:
		return lipgloss.NewStyle().Foreground(ui.SessionStatusColor(s.Status))
	}
	if selected {
		return lipgloss.NewStyle().Foreground(theme.Current.TextEmphasis).Bold(true)
	}
	return lipgloss.NewStyle().Foreground(theme.Current.Text)
}

func (m Model) renderRow(s session.Session, selected bool, width int) string {
	var bg lipgloss.TerminalColor
	gutter := ui.Span{Text: "  "}
	if selected {
		bg = theme.Current.Selection
		gutter = ui.Span{Text: "▌ ", Style: lipgloss.NewStyle().Foreground(theme.Current.Primary)}
	}

	var attached ui.Span
	if s.IsAttached {
		attached = ui.Span{Text: "◆", Style: lipgloss.NewStyle().Foreground(theme.Current.Primary)}
	}

	ready, draft := ui.PRCounts(s.PullRequests)
	muted := lipgloss.NewStyle().Foreground(theme.Current.TextMuted)

	return ui.Row([]ui.Cell{
		{Spans: []ui.Span{gutter}, Width: gutterColW},
		{Spans: []ui.Span{ui.ClaudeGlyph(s.AttentionStatus())}, Width: glyphColW},
		{Spans: []ui.Span{{Text: sessionDisplayName(s), Style: nameStyle(s, selected)}}},
		{Spans: []ui.Span{attached}, Width: attachedColW},
		{Spans: ui.PRCountSpans(ready, draft), Width: prColW},
		{Spans: []ui.Span{{Text: ui.ShortAgo(s.LastUsedAt), Style: muted}}, Width: timeColW, Align: ui.AlignRight},
	}, width, bg)
}
