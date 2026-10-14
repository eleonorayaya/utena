package branchpicker

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

const defaultColW = 8

type branchItem struct {
	name      string
	isDefault bool
}

func (i branchItem) FilterValue() string { return i.name }

func (i branchItem) Cells(selected bool) []ui.Cell {
	style := lipgloss.NewStyle().Foreground(theme.Current.Text)
	if selected {
		style = lipgloss.NewStyle().Foreground(theme.Current.TextEmphasis).Bold(true)
	}
	var marker ui.Span
	if i.isDefault {
		marker = ui.Span{Text: "default", Style: lipgloss.NewStyle().Foreground(theme.Current.TextMuted)}
	}
	return []ui.Cell{
		{Spans: []ui.Span{{Text: i.name, Style: style}}},
		{Spans: []ui.Span{marker}, Width: defaultColW, Align: ui.AlignRight},
	}
}
