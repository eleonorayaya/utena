package branchpicker

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

type branchItem struct {
	name   string
	remote bool
}

func (i branchItem) Title() string { return i.name }
func (i branchItem) Description() string {
	if i.remote {
		return "remote"
	}
	return ""
}
func (i branchItem) FilterValue() string { return i.name }

type compactBranchDelegate struct{}

func (d compactBranchDelegate) Height() int                             { return 1 }
func (d compactBranchDelegate) Spacing() int                            { return 0 }
func (d compactBranchDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d compactBranchDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	i, ok := item.(branchItem)
	if !ok {
		return
	}

	selected := m.Index() == index

	cursor := "  "
	if selected {
		cursor = lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true).Render("› ")
	}

	var nameStyle lipgloss.Style
	if selected {
		nameStyle = lipgloss.NewStyle().Foreground(theme.Current.TextEmphasis).Bold(true)
	} else {
		nameStyle = lipgloss.NewStyle().Foreground(theme.Current.Text)
	}

	row := cursor + nameStyle.Render(i.name)
	if i.remote {
		remoteStyle := lipgloss.NewStyle().Foreground(theme.Current.TextMuted)
		row += "  " + remoteStyle.Render("(remote)")
	}
	fmt.Fprint(w, row)
}
