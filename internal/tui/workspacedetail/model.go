package workspacedetail

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/tui/prlist"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/workspace"
)

func titleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(theme.Current.TextEmphasis)
}

func labelStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Width(12)
}

func valueStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.Text)
}

type Model struct {
	workspace *workspace.Workspace
	actionErr string
	width     int
	height    int
}

func New() Model {
	return Model{}
}

func (m Model) Init() (Model, tea.Cmd) {
	return m, nil
}

func (m Model) Keys() help.KeyMap {
	return keys
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case SelectMsg:
		ws := msg.Workspace
		m.workspace = &ws
		m.actionErr = ""
		return m, nil
	case provider.WorkspacesStateUpdatedMsg:
		if m.workspace != nil {
			for _, ws := range msg.Workspaces {
				if ws.ID == m.workspace.ID {
					wsCopy := ws
					m.workspace = &wsCopy
					break
				}
			}
		}
		return m, nil
	case provider.ErrMsg:
		m.actionErr = msg.Err.Error()
		return m, nil
	case tea.KeyMsg:
		return m.onKeyMsg(msg)
	}
	return m, nil
}

func (m Model) onKeyMsg(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.PRs):
		if m.workspace == nil || m.workspace.RepoID == nil {
			return m, nil
		}
		return m, tea.Sequence(
			router.NavigateTo(router.PRListView),
			prlist.Select(m.workspace.ID),
		)
	case key.Matches(msg, keys.Migrate):
		if m.workspace == nil || !m.workspace.IsGitRepo || m.workspace.IsBare {
			return m, nil
		}
		m.actionErr = ""
		return m, provider.MigrateWorkspaceToBare(m.workspace.ID)
	case key.Matches(msg, keys.Back):
		return m, router.Back()
	}
	return m, nil
}

func (m Model) View() string {
	if m.workspace == nil {
		return "No workspace selected"
	}

	var b strings.Builder
	ws := m.workspace

	b.WriteString(titleStyle().Render(ws.Name))
	b.WriteString("\n\n")

	b.WriteString(labelStyle().Render("Path") + valueStyle().Render(ws.Path) + "\n")

	if ws.IsGitRepo {
		repoName := "linked"
		if ws.Repo != nil {
			repoName = ws.Repo.FullName
		}
		b.WriteString(labelStyle().Render("Repository") + valueStyle().Render(repoName) + "\n")

		bareStatus := "no  (press m to migrate)"
		if ws.IsBare {
			bareStatus = "yes"
		}
		b.WriteString(labelStyle().Render("Bare") + valueStyle().Render(bareStatus) + "\n")
	} else {
		b.WriteString(labelStyle().Render("Repository") + valueStyle().Render("not a git repo") + "\n")
	}

	if !ws.LastUsedAt.IsZero() {
		b.WriteString(labelStyle().Render("Last used") + valueStyle().Render(common.TimeAgo(ws.LastUsedAt)) + "\n")
	}

	if m.actionErr != "" {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(theme.Current.Error).Render("Error: "+m.actionErr) + "\n")
	}

	return b.String()
}
