package prlist

import (
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/git"
	ulist "github.com/eleonorayaya/utena/internal/tui/list"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
)

type Model struct {
	list        list.Model
	workspaceID uint
	prs         []git.PullRequest
	height      int
}

func New() Model {
	return Model{list: ulist.New("Pull Requests")}
}

func (m Model) Init() (Model, tea.Cmd) {
	return m, nil
}

func (m Model) Filtering() bool {
	return m.list.FilterState() == list.Filtering
}

func (m Model) Keys() help.KeyMap {
	return keys
}

func (m *Model) rebuildItems() tea.Cmd {
	var items []list.Item
	for _, pr := range m.prs {
		items = append(items, prItem{pr: pr})
	}
	return m.list.SetItems(items)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.list.SetWidth(msg.Width)
		m.list.SetHeight(msg.Height)
		return m, nil
	case SelectMsg:
		m.workspaceID = msg.WorkspaceID
		return m, provider.FetchPRs(msg.WorkspaceID, "open")
	case provider.PRsStateUpdatedMsg:
		if msg.WorkspaceID == m.workspaceID {
			m.prs = msg.PullRequests
			return m, m.rebuildItems()
		}
		return m, nil
	case provider.ErrMsg:
		return m, m.list.NewStatusMessage(msg.Err.Error())
	case tea.KeyMsg:
		var cmd tea.Cmd
		var handled bool
		m, cmd, handled = m.OnKeyMsg(msg)
		if handled {
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) OnKeyMsg(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if m.list.FilterState() == list.Filtering {
		return m, nil, false
	}
	switch {
	case key.Matches(msg, keys.Open):
		item, ok := m.list.SelectedItem().(prItem)
		if !ok || item.pr.HTMLURL == "" {
			return m, nil, false
		}
		return m, openURL(item.pr.HTMLURL), true
	case key.Matches(msg, keys.Back):
		return m, router.Back(), true
	}
	return m, nil, false
}

func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("open", url)
		if err := cmd.Start(); err == nil {
			go cmd.Wait()
		}
		return nil
	}
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

func (m Model) View() string {
	v := strings.TrimRight(m.list.View(), "\n")
	return padToHeight(v, m.height-1)
}
