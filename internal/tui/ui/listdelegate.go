package ui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

const GutterWidth = 2

type RowItem interface {
	list.Item
	Cells(selected bool) []Cell
}

type SectionItem struct {
	Title string
	Count int
}

func (SectionItem) FilterValue() string { return "" }

type SpacerItem struct{}

func (SpacerItem) FilterValue() string { return "" }

func Selectable(item list.Item) bool {
	_, ok := item.(RowItem)
	return ok
}

type ListDelegate struct{}

func (ListDelegate) Height() int                             { return 1 }
func (ListDelegate) Spacing() int                            { return 0 }
func (ListDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (ListDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	width := m.Width()
	switch it := item.(type) {
	case SectionItem:
		fmt.Fprint(w, clampWidth("  "+SectionHeader(it.Title, it.Count), width))
	case RowItem:
		fmt.Fprint(w, SelectableRow(it.Cells(index == m.Index()), index == m.Index(), width))
	}
}

func SelectableRow(cells []Cell, selected bool, width int) string {
	var bg lipgloss.TerminalColor
	gutter := Span{Text: "  "}
	if selected {
		bg = theme.Current.Selection
		gutter = Span{Text: "▌ ", Style: fg(theme.Current.Primary)}
	}
	all := append([]Cell{{Spans: []Span{gutter}, Width: GutterWidth}}, cells...)
	return Row(all, width, bg)
}
