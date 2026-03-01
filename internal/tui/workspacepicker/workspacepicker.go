package workspacepicker

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type Model struct {
	list              list.Model
	sortActiveFirst   bool
	activeWorkspaceID string
}

func New(title string, sortActiveFirst bool) Model {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = title
	l.KeyMap.Quit.SetEnabled(false)
	l.SetShowHelp(false)
	return Model{
		list:            l,
		sortActiveFirst: sortActiveFirst,
	}
}

func (m *Model) SetSize(width, height int) {
	m.list.SetWidth(width)
	m.list.SetHeight(height)
}

func (m Model) Init() (Model, tea.Cmd) {
	return m, nil
}

func (m Model) OnWindowSizeMsg(msg tea.WindowSizeMsg) (Model, tea.Cmd) {
	m.list.SetWidth(msg.Width)
	m.list.SetHeight(msg.Height)
	return m, nil
}

func (m Model) Keys() help.KeyMap {
	return Keys
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case provider.WorkspacesStateUpdatedMsg:
		m.activeWorkspaceID = msg.ActiveWorkspaceID
		workspaces := msg.Workspaces

		if m.sortActiveFirst && m.activeWorkspaceID != "" {
			sorted := make([]workspace.Workspace, 0, len(workspaces))
			var rest []workspace.Workspace
			for _, ws := range workspaces {
				if ws.ID == m.activeWorkspaceID {
					sorted = append(sorted, ws)
				} else {
					rest = append(rest, ws)
				}
			}
			workspaces = append(sorted, rest...)
		}

		items := make([]list.Item, len(workspaces))
		for i, ws := range workspaces {
			items[i] = workspaceItem{workspace: ws}
		}
		return m, m.list.SetItems(items)

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
	case key.Matches(msg, Keys.Select):
		if item, ok := m.list.SelectedItem().(workspaceItem); ok {
			ws := item.workspace
			return m, func() tea.Msg {
				return SelectedMsg{Workspace: ws}
			}, true
		}
	case key.Matches(msg, Keys.AddDir):
		return m, func() tea.Msg { return AddDirectoryMsg{} }, true
	}
	return m, nil, false
}

func (m Model) View() string {
	return m.list.View()
}
