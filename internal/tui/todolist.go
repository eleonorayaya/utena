package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/todo"
)

type todoItem struct {
	todo todo.Todo
}

func (i todoItem) Title() string { return i.todo.Name }
func (i todoItem) Description() string {
	desc := i.todo.WorkspaceName
	if desc == "" {
		desc = "no workspace"
	}
	if i.todo.Description != "" {
		desc += " · " + i.todo.Description
	}
	return desc
}
func (i todoItem) FilterValue() string { return i.todo.Name }

type TodoListModel struct {
	list              list.Model
	todos             []todo.Todo
	showAllWorkspaces bool
	activeWorkspaceID string
	pendingDeleteID   string
}

func NewTodoListModel() TodoListModel {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Todos"
	l.KeyMap.Quit.SetEnabled(false)
	l.SetShowHelp(false)
	return TodoListModel{list: l}
}

func (m TodoListModel) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return requestTodosStateMsg{} },
		func() tea.Msg { return requestWorkspacesStateMsg{} },
	)
}

func (m TodoListModel) Filtering() bool {
	return m.list.FilterState() == list.Filtering
}

func (m TodoListModel) Keys() help.KeyMap {
	return todoListKeys
}

func (m *TodoListModel) rebuildItems() tea.Cmd {
	var items []list.Item
	for _, t := range m.todos {
		if !m.showAllWorkspaces && m.activeWorkspaceID != "" && t.WorkspaceID != m.activeWorkspaceID {
			continue
		}
		items = append(items, todoItem{todo: t})
	}
	return m.list.SetItems(items)
}

func (m *TodoListModel) updateTitle() {
	if m.showAllWorkspaces {
		m.list.Title = "Todos (all workspaces)"
	} else {
		m.list.Title = "Todos"
	}
}

func (m TodoListModel) Update(msg tea.Msg) (TodoListModel, tea.Cmd) {
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		m.list.SetWidth(wsm.Width)
		m.list.SetHeight(wsm.Height)
		return m, nil
	}

	switch msg := msg.(type) {
	case todosStateUpdatedMsg:
		m.todos = msg.todos
		cmd := m.rebuildItems()
		return m, cmd

	case workspacesStateUpdatedMsg:
		m.activeWorkspaceID = msg.activeWorkspaceID
		cmd := m.rebuildItems()
		return m, cmd

	case errMsg:
		return m, m.list.NewStatusMessage(msg.err.Error())

	case tea.KeyMsg:
		if m.list.FilterState() == list.Filtering {
			break
		}
		if !key.Matches(msg, todoListKeys.Delete) {
			m.pendingDeleteID = ""
		}
		switch {
		case key.Matches(msg, todoListKeys.New):
			return m, func() tea.Msg { return navigateMsg{target: todoFormView} }
		case key.Matches(msg, todoListKeys.Back):
			return m, func() tea.Msg { return navigateMsg{target: sessionListView} }
		case key.Matches(msg, todoListKeys.Delete):
			item, ok := m.list.SelectedItem().(todoItem)
			if !ok {
				break
			}
			if m.pendingDeleteID == item.todo.ID {
				m.pendingDeleteID = ""
				id := item.todo.ID
				return m, func() tea.Msg {
					return deleteTodoIntentMsg{id: id}
				}
			}
			m.pendingDeleteID = item.todo.ID
			return m, m.list.NewStatusMessage("press d again to delete " + item.todo.Name)
		case key.Matches(msg, todoListKeys.ToggleAll):
			m.showAllWorkspaces = !m.showAllWorkspaces
			m.updateTitle()
			cmd := m.rebuildItems()
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m TodoListModel) View() string {
	return m.list.View()
}
