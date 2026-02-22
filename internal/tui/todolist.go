package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/todo"
)

type switchToTodoListMsg struct{}

type todoDataLoadedMsg struct {
	todos    []todo.Todo
	sessions []session.Session
}

type deleteTodoMsg struct {
	id string
}

type todoDeletedMsg struct {
	id string
}

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
	list               list.Model
	todos              []todo.Todo
	showAllWorkspaces  bool
	currentWorkspaceID string
	pendingDeleteID    string
}

func NewTodoListModel() TodoListModel {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Todos"
	l.KeyMap.Quit.SetEnabled(false)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{newTodoKey, deleteTodoKey, toggleAllKey, backKey}
	}
	return TodoListModel{list: l}
}

func (m *TodoListModel) SetSize(width, height int) {
	m.list.SetWidth(width)
	m.list.SetHeight(height)
}

func (m *TodoListModel) deriveCurrentWorkspace(sessions []session.Session) {
	for _, s := range sessions {
		if s.IsAttached {
			m.currentWorkspaceID = s.WorkspaceID
			return
		}
	}
	m.currentWorkspaceID = ""
}

func (m *TodoListModel) rebuildItems() tea.Cmd {
	var items []list.Item
	for _, t := range m.todos {
		if !m.showAllWorkspaces && m.currentWorkspaceID != "" && t.WorkspaceID != m.currentWorkspaceID {
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
	switch msg := msg.(type) {
	case todoDataLoadedMsg:
		m.todos = msg.todos
		m.deriveCurrentWorkspace(msg.sessions)
		cmd := m.rebuildItems()
		return m, cmd

	case tea.KeyMsg:
		if m.list.FilterState() == list.Filtering {
			break
		}
		if !key.Matches(msg, deleteTodoKey) {
			m.pendingDeleteID = ""
		}
		switch {
		case key.Matches(msg, newTodoKey):
			return m, func() tea.Msg { return switchToTodoWorkspacePickerMsg{} }
		case key.Matches(msg, deleteTodoKey):
			item, ok := m.list.SelectedItem().(todoItem)
			if !ok {
				break
			}
			if m.pendingDeleteID == item.todo.ID {
				m.pendingDeleteID = ""
				return m, func() tea.Msg {
					return deleteTodoMsg{id: item.todo.ID}
				}
			}
			m.pendingDeleteID = item.todo.ID
			return m, m.list.NewStatusMessage("press d again to delete " + item.todo.Name)
		case key.Matches(msg, toggleAllKey):
			m.showAllWorkspaces = !m.showAllWorkspaces
			m.updateTitle()
			cmd := m.rebuildItems()
			return m, cmd
		case key.Matches(msg, backKey):
			return m, func() tea.Msg { return switchToSessionListMsg{} }
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m TodoListModel) View() string {
	return m.list.View()
}
