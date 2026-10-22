package sessionpicker

import (
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

type sessionItem struct {
	session session.Session
}

func (i sessionItem) FilterValue() string { return i.session.DisplayLabel() }

func (i sessionItem) Cells(selected bool) []ui.Cell {
	detail := string(i.session.Status)
	if i.session.IsAttached {
		detail = "attached"
	}
	return []ui.Cell{
		{Spans: []ui.Span{{Text: i.session.DisplayLabel(), Style: ui.NameStyle(selected)}}},
		{Spans: []ui.Span{{Text: detail, Style: ui.FG(theme.Current.TextMuted)}}, Width: 9},
		{Spans: []ui.Span{{Text: ui.ShortAgo(i.session.LastUsedAt), Style: ui.FG(theme.Current.TextMuted)}}, Width: 4, Align: ui.AlignRight},
	}
}
