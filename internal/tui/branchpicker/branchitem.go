package branchpicker

import "github.com/eleonorayaya/utena/internal/tui/ui"

const defaultColW = 8

type branchItem struct {
	name      string
	isDefault bool
}

func (i branchItem) FilterValue() string { return i.name }

func (i branchItem) Cells(selected bool) []ui.Cell {
	var marker ui.Span
	if i.isDefault {
		marker = ui.Span{Text: "default", Style: ui.Muted()}
	}
	return []ui.Cell{
		{Spans: []ui.Span{{Text: i.name, Style: ui.NameStyle(selected)}}},
		{Spans: []ui.Span{marker}, Width: defaultColW, Align: ui.AlignRight},
	}
}
