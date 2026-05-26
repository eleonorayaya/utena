package workspacepicker

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type workspaceItem struct {
	workspace workspace.Workspace
	selected  bool
}

func (i workspaceItem) Title() string       { return i.workspace.Name }
func (i workspaceItem) Description() string { return AbbreviatePath(i.workspace.Path) }
func (i workspaceItem) FilterValue() string { return i.workspace.Name }

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

type compactPickerDelegate struct{}

func (d compactPickerDelegate) Height() int                             { return 1 }
func (d compactPickerDelegate) Spacing() int                            { return 0 }
func (d compactPickerDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d compactPickerDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	i, ok := item.(workspaceItem)
	if !ok {
		return
	}

	focused := m.Index() == index

	cursor := "  "
	if focused {
		cursor = lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true).Render("› ")
	}

	checkStyle := lipgloss.NewStyle().Foreground(theme.Current.AccentMint).Bold(true)
	var check string
	if i.selected {
		check = checkStyle.Render("✓ ")
	} else {
		check = "  "
	}

	name := i.workspace.Name
	if i.workspace.IsHidden {
		name += " [hidden]"
	}

	var nameStyle lipgloss.Style
	if focused {
		nameStyle = lipgloss.NewStyle().Foreground(theme.Current.TextEmphasis).Bold(true)
	} else {
		nameStyle = lipgloss.NewStyle().Foreground(theme.Current.Text)
	}

	path := AbbreviatePath(i.workspace.Path)
	pathStyle := lipgloss.NewStyle().Foreground(theme.Current.TextMuted)

	fmt.Fprint(w, cursor+check+nameStyle.Render(name)+"  "+pathStyle.Render(path))
}
