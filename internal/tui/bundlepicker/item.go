package bundlepicker

import (
	"strings"

	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

type bundleItem struct {
	name    string
	plugins []string
	servers []string
	unknown bool
	checked bool
	nameW   int
}

func (i bundleItem) FilterValue() string { return i.name }

func (i bundleItem) Cells(selected bool) []ui.Cell {
	parts := append([]string{}, i.plugins...)
	for _, s := range i.servers {
		parts = append(parts, s+" (mcp)")
	}
	detail := ui.Span{Text: strings.Join(parts, ", "), Style: ui.FG(theme.Current.TextMuted)}
	if i.unknown {
		detail = ui.Span{Text: "unknown bundle", Style: ui.FG(theme.Current.Error)}
	}
	return []ui.Cell{
		ui.CheckCell(i.checked),
		{Spans: []ui.Span{{Text: i.name, Style: ui.NameStyle(selected)}}, Width: i.nameW},
		{Spans: []ui.Span{detail}},
	}
}
