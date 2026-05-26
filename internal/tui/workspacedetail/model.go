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

func ruleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.SurfaceVariant)
}

func sectionStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true)
}

func padToHeight(s string, h int) string {
	if h <= 0 {
		return s
	}
	n := strings.Count(s, "\n")
	if n >= h {
		return s
	}
	return s + strings.Repeat("\n", h-n)
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
	b.WriteString("\n")

	ruleWidth := m.width
	if ruleWidth < 1 {
		ruleWidth = 40
	}
	b.WriteString(ruleStyle().Render(strings.Repeat("─", ruleWidth)))
	b.WriteString("\n\n")

	pathStyle := lipgloss.NewStyle().Foreground(theme.Current.Path)
	b.WriteString(labelStyle().Render("Path") + pathStyle.Render(ws.Path) + "\n")

	if !ws.LastUsedAt.IsZero() {
		b.WriteString(labelStyle().Render("Last used") + valueStyle().Render(common.TimeAgo(ws.LastUsedAt)) + "\n")
	}

	if ws.IsGitRepo {
		b.WriteString("\n" + sectionStyle().Render("⎇ Repository") + "\n")
		repoName := "linked"
		if ws.Repo != nil {
			repoName = ws.Repo.FullName
		}
		b.WriteString(labelStyle().Render("Name") + valueStyle().Render(repoName) + "\n")

		bareStatus := "no"
		if ws.IsBare {
			bareStatus = "yes"
		}
		b.WriteString(labelStyle().Render("Bare") + valueStyle().Render(bareStatus) + "\n")
		if !ws.IsBare {
			b.WriteString(labelStyle().Render("") + lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Render("press m to migrate to bare") + "\n")
		}
	} else {
		b.WriteString("\n" + sectionStyle().Render("⎇ Repository") + "\n")
		b.WriteString(labelStyle().Render("") + lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Render("not a git repository") + "\n")
	}

	if m.actionErr != "" {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(theme.Current.Error).Render("[!] "+m.actionErr) + "\n")
	}

	return padToHeight(b.String(), m.height-1)
}
