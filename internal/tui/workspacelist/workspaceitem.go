package workspacelist

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/workspacepicker"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type workspaceItem struct {
	workspace workspace.Workspace
}

func (i workspaceItem) Title() string       { return i.workspace.Name }
func (i workspaceItem) Description() string { return workspacepicker.AbbreviatePath(i.workspace.Path) }
func (i workspaceItem) FilterValue() string { return i.workspace.Name }

type compactDelegate struct{}

func (d compactDelegate) Height() int                             { return 1 }
func (d compactDelegate) Spacing() int                            { return 0 }
func (d compactDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d compactDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	i, ok := item.(workspaceItem)
	if !ok {
		return
	}

	selected := m.Index() == index

	cursor := "  "
	if selected {
		cursor = lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true).Render("› ")
	}

	name := i.workspace.Name
	if i.workspace.IsGitRepo {
		name += " ⎇"
	}
	if i.workspace.IsHidden {
		name += " [hidden]"
	}

	var nameStyle lipgloss.Style
	if selected {
		nameStyle = lipgloss.NewStyle().Foreground(theme.Current.TextEmphasis).Bold(true)
	} else {
		nameStyle = lipgloss.NewStyle().Foreground(theme.Current.Text)
	}

	path := workspacepicker.AbbreviatePath(i.workspace.Path)
	pathStyle := lipgloss.NewStyle().Foreground(theme.Current.TextMuted)

	fmt.Fprint(w, cursor+nameStyle.Render(name)+"  "+pathStyle.Render(path)+statusSuffix(i.workspace))
}

func statusSuffix(ws workspace.Workspace) string {
	switch ws.Status {
	case workspace.StatusCloning, workspace.StatusMigrating:
		label := string(ws.Status)
		if ws.Progress != "" {
			label += ": " + ws.Progress
		}
		return "  " + lipgloss.NewStyle().Foreground(theme.Current.StatusActive).Render("· "+label)
	case workspace.StatusFailed:
		return "  " + lipgloss.NewStyle().Foreground(theme.Current.Error).Render("· failed")
	default:
		return ""
	}
}
