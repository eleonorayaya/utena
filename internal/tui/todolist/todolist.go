package todolist

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/todo"
	"github.com/eleonorayaya/utena/internal/tui/provider"
)

type Model struct {
	list              list.Model
	todos             []todo.Todo
	showAllWorkspaces bool
	activeWorkspaceID string
	pendingDeleteID   string
}

func New() Model {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Todos"
	l.KeyMap.Quit.SetEnabled(false)
	l.SetShowHelp(false)
	return Model{list: l}
}

func (m Model) Init() (Model, tea.Cmd) {
	return m, tea.Batch(provider.FetchTodos(), provider.FetchWorkspaces())
}

func (m Model) Filtering() bool {
	return m.list.FilterState() == list.Filtering
}

func (m Model) Keys() help.KeyMap {
	return keys
}

func (m *Model) rebuildItems() tea.Cmd {
	var items []list.Item
	for _, t := range m.todos {
		if !m.showAllWorkspaces && m.activeWorkspaceID != "" && t.WorkspaceID != m.activeWorkspaceID {
			continue
		}
		items = append(items, todoItem{todo: t})
	}
	return m.list.SetItems(items)
}

func (m *Model) updateTitle() {
	if m.showAllWorkspaces {
		m.list.Title = "Todos (all workspaces)"
	} else {
		m.list.Title = "Todos"
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.OnWindowSizeMsg(msg)
	case provider.TodosStateUpdatedMsg:
		m.todos = msg.Todos
		return m, m.rebuildItems()
	case provider.WorkspacesStateUpdatedMsg:
		m.activeWorkspaceID = msg.ActiveWorkspaceID
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

func (m Model) OnWindowSizeMsg(msg tea.WindowSizeMsg) (Model, tea.Cmd) {
	m.list.SetWidth(msg.Width)
	m.list.SetHeight(msg.Height)
	return m, nil
}

func (m Model) OnKeyMsg(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if m.list.FilterState() == list.Filtering {
		return m, nil, false
	}
	if !key.Matches(msg, keys.Delete) {
		m.pendingDeleteID = ""
	}
	switch {
	case key.Matches(msg, keys.New):
		return m, func() tea.Msg { return NewTodoMsg{} }, true
	case key.Matches(msg, keys.Back):
		return m, func() tea.Msg { return BackMsg{} }, true
	case key.Matches(msg, keys.Delete):
		item, ok := m.list.SelectedItem().(todoItem)
		if !ok {
			return m, nil, false
		}
		if m.pendingDeleteID == item.todo.ID {
			m.pendingDeleteID = ""
			return m, provider.DeleteTodo(item.todo.ID), true
		}
		m.pendingDeleteID = item.todo.ID
		return m, m.list.NewStatusMessage("press d again to delete " + item.todo.Name), true
	case key.Matches(msg, keys.ToggleAll):
		m.showAllWorkspaces = !m.showAllWorkspaces
		m.updateTitle()
		return m, m.rebuildItems(), true
	}
	return m, nil, false
}

func (m Model) View() string {
	return m.list.View()
}
