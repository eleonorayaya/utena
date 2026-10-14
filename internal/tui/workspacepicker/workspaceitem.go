package workspacepicker

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/eleonorayaya/utena/internal/workspace"
)

const (
	checkColW = 4
	KindColW  = 13
	AgoColW   = 4
)

func AbbreviatePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

func fg(c lipgloss.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(c)
}

func KindSpan(ws workspace.Workspace) ui.Span {
	switch ws.Status {
	case workspace.StatusCloning:
		return ui.Span{Text: "◌ cloning…", Style: fg(theme.Current.StatusActive)}
	case workspace.StatusMigrating:
		return ui.Span{Text: "◌ migrating…", Style: fg(theme.Current.StatusActive)}
	case workspace.StatusFailed:
		return ui.Span{Text: "✕ failed", Style: fg(theme.Current.Error)}
	}
	muted := fg(theme.Current.TextMuted)
	switch {
	case !ws.IsGitRepo:
		return ui.Span{Text: "not git", Style: muted}
	case ws.IsBare:
		return ui.Span{Text: "bare", Style: muted}
	default:
		return ui.Span{Text: "not bare", Style: muted}
	}
}

func NameStyle(selected bool) lipgloss.Style {
	if selected {
		return fg(theme.Current.TextEmphasis).Bold(true)
	}
	return fg(theme.Current.Text)
}

func AgoSpan(ws workspace.Workspace) ui.Span {
	return ui.Span{Text: ui.ShortAgo(ws.LastUsedAt), Style: fg(theme.Current.TextMuted)}
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
		box := ui.Span{Text: "[ ]", Style: fg(theme.Current.TextMuted)}
		if i.checked {
			box = ui.Span{Text: "[x]", Style: fg(theme.Current.Primary)}
		}
		cells = append(cells, ui.Cell{Spans: []ui.Span{box}, Width: checkColW})
	}
	return append(cells,
		ui.Cell{Spans: []ui.Span{{Text: i.workspace.Name, Style: NameStyle(selected)}}},
		ui.Cell{Spans: []ui.Span{KindSpan(i.workspace)}, Width: KindColW},
		ui.Cell{Spans: []ui.Span{AgoSpan(i.workspace)}, Width: AgoColW, Align: ui.AlignRight},
	)
}
