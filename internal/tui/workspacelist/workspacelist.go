package workspacelist

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	ulist "github.com/eleonorayaya/utena/internal/tui/list"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/workspacedetail"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type Model struct {
	list       list.Model
	workspaces []workspace.Workspace
}

func New() Model {
	return Model{list: ulist.New("Workspaces")}
}

func (m Model) Init() (Model, tea.Cmd) {
	return m, provider.FetchWorkspaces()
}

func (m Model) Filtering() bool {
	return m.list.FilterState() == list.Filtering
}

func (m Model) Keys() help.KeyMap {
	return keys
}

func (m *Model) rebuildItems() tea.Cmd {
	var items []list.Item
	for _, ws := range m.workspaces {
		items = append(items, workspaceItem{workspace: ws})
	}
	return m.list.SetItems(items)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetWidth(msg.Width)
		m.list.SetHeight(msg.Height)
		return m, nil
	case provider.WorkspacesStateUpdatedMsg:
		m.workspaces = msg.Workspaces
		return m, m.rebuildItems()
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
	case key.Matches(msg, keys.Select):
		item, ok := m.list.SelectedItem().(workspaceItem)
		if !ok {
			return m, nil, false
		}
		return m, tea.Sequence(
			router.NavigateTo(router.WorkspaceDetailView),
			workspacedetail.Select(item.workspace),
		), true
	case key.Matches(msg, keys.Back):
		return m, router.Back(), true
	}
	return m, nil, false
}

func (m Model) View() string {
	return m.list.View()
}
