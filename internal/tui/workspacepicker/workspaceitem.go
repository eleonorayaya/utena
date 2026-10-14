package workspacepicker

import (
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/eleonorayaya/utena/internal/workspace"
)

const (
	checkColW = 4
	KindColW  = 13
	AgoColW   = 4
)

func KindSpan(ws workspace.Workspace) ui.Span {
	switch ws.Status {
	case workspace.StatusCloning:
		return ui.Span{Text: "◌ cloning…", Style: ui.FG(theme.Current.StatusActive)}
	case workspace.StatusMigrating:
		return ui.Span{Text: "◌ migrating…", Style: ui.FG(theme.Current.StatusActive)}
	case workspace.StatusFailed:
		return ui.Span{Text: "✕ failed", Style: ui.FG(theme.Current.Error)}
	}
	muted := ui.FG(theme.Current.TextMuted)
	switch {
	case !ws.IsGitRepo:
		return ui.Span{Text: "not git", Style: muted}
	case ws.IsBare:
		return ui.Span{Text: "bare", Style: muted}
	default:
		return ui.Span{Text: "not bare", Style: muted}
	}
}

func AgoSpan(ws workspace.Workspace) ui.Span {
	return ui.Span{Text: ui.ShortAgo(ws.LastUsedAt), Style: ui.FG(theme.Current.TextMuted)}
}

type workspaceItem struct {
	workspace workspace.Workspace
	multi     bool
	checked   bool
}

func (i workspaceItem) FilterValue() string { return i.workspace.Name }

func (i workspaceItem) Cells(selected bool) []ui.Cell {
	var cells []ui.Cell
	if i.multi {
		box := ui.Span{Text: "[ ]", Style: ui.FG(theme.Current.TextMuted)}
		if i.checked {
			box = ui.Span{Text: "[x]", Style: ui.FG(theme.Current.Primary)}
		}
		cells = append(cells, ui.Cell{Spans: []ui.Span{box}, Width: checkColW})
	}
	return append(cells,
		ui.Cell{Spans: []ui.Span{{Text: i.workspace.Name, Style: ui.NameStyle(selected)}}},
		ui.Cell{Spans: []ui.Span{KindSpan(i.workspace)}, Width: KindColW},
		ui.Cell{Spans: []ui.Span{AgoSpan(i.workspace)}, Width: AgoColW, Align: ui.AlignRight},
	)
}
