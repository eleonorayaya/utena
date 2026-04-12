package sessionlist

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/statusview"
)

var _ list.ItemDelegate = sessionItemDelegate{}

type sessionItemDelegate struct{}

func (d sessionItemDelegate) Height() int                             { return 4 }
func (d sessionItemDelegate) Spacing() int                            { return 0 }
func (d sessionItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d sessionItemDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	si, ok := item.(sessionItem)
	if !ok {
		return
	}
	width := m.Width()
	if width < 1 {
		return
	}
	isSelected := index == m.Index()
	tab := statusview.NewSessionTab(si.session).
		WithWidth(width).
		WithoutWindows().
		WithSelected(isSelected)
	fmt.Fprint(w, tab.View())
}
