package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type switchToTodoWorkspacePickerMsg struct{}

type switchToNewTodoMsg struct {
	workspace *workspace.Workspace
}

type TodoWorkspacePickerModel struct {
	list               list.Model
	currentWorkspaceID string
}

func NewTodoWorkspacePickerModel() TodoWorkspacePickerModel {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Select workspace for todo"
	l.KeyMap.Quit.SetEnabled(false)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{selectKey, backKey, addWorkspaceKey}
	}
	return TodoWorkspacePickerModel{list: l}
}

func (m *TodoWorkspacePickerModel) SetSize(width, height int) {
	m.list.SetWidth(width)
	m.list.SetHeight(height)
}

func (m TodoWorkspacePickerModel) Update(msg tea.Msg) (TodoWorkspacePickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case workspacesLoadedMsg:
		sorted := make([]workspace.Workspace, 0, len(msg.workspaces))
		var rest []workspace.Workspace
		for _, ws := range msg.workspaces {
			if ws.ID == m.currentWorkspaceID {
				sorted = append(sorted, ws)
			} else {
				rest = append(rest, ws)
			}
		}
		sorted = append(sorted, rest...)

		items := make([]list.Item, len(sorted))
		for i, ws := range sorted {
			items[i] = workspaceItem{workspace: ws}
		}
		cmd := m.list.SetItems(items)
		return m, cmd

	case tea.KeyMsg:
		if m.list.FilterState() == list.Filtering {
			break
		}
		switch {
		case key.Matches(msg, selectKey):
			if item, ok := m.list.SelectedItem().(workspaceItem); ok {
				ws := item.workspace
				return m, func() tea.Msg {
					return switchToNewTodoMsg{workspace: &ws}
				}
			}
		case key.Matches(msg, backKey):
			return m, func() tea.Msg {
				return switchToTodoListMsg{}
			}
		case key.Matches(msg, addWorkspaceKey):
			return m, func() tea.Msg { return switchToFilePickerMsg{} }
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m TodoWorkspacePickerModel) View() string {
	return m.list.View()
}
