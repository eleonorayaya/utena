package workspacedetail

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/prlist"
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
	width     int
	height    int
}

func New() Model {
	return Model{}
}

func (m Model) Init() (Model, tea.Cmd) {
	m.workspace = nil
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
	} else {
		b.WriteString(labelStyle().Render("Repository") + valueStyle().Render("not a git repo") + "\n")
	}

	if !ws.LastUsedAt.IsZero() {
		b.WriteString(labelStyle().Render("Last used") + valueStyle().Render(timeAgo(ws.LastUsedAt)) + "\n")
	}

	return b.String()
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		return fmt.Sprintf("%dh ago", h)
	default:
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	}
}
