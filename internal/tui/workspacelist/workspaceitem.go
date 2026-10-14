package workspacelist

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/eleonorayaya/utena/internal/tui/workspacepicker"
	"github.com/eleonorayaya/utena/internal/workspace"
)

const maxRepoColW = 28

type workspaceItem struct {
	workspace workspace.Workspace
	repoW     int
}

func (i workspaceItem) FilterValue() string { return i.workspace.Name }

func repoName(ws workspace.Workspace) string {
	if ws.Repo != nil && ws.Repo.FullName != "" {
		return ws.Repo.FullName
	}
	return "—"
}

func (i workspaceItem) Cells(selected bool) []ui.Cell {
	muted := lipgloss.NewStyle().Foreground(theme.Current.TextMuted)
	return []ui.Cell{
		{Spans: []ui.Span{{Text: i.workspace.Name, Style: workspacepicker.NameStyle(selected)}}},
		{Spans: []ui.Span{{Text: repoName(i.workspace), Style: muted}}, Width: i.repoW},
		{Spans: []ui.Span{workspacepicker.KindSpan(i.workspace)}, Width: workspacepicker.KindColW},
		{Spans: []ui.Span{workspacepicker.AgoSpan(i.workspace)}, Width: workspacepicker.AgoColW, Align: ui.AlignRight},
	}
}
