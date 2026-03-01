package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/todo"
)

type TodosProvider struct {
	todos []todo.Todo
}

func NewTodosProvider() TodosProvider {
	return TodosProvider{}
}

func (p TodosProvider) emitState() tea.Cmd {
	todos := p.todos
	return func() tea.Msg {
		return todosStateUpdatedMsg{todos: todos}
	}
}

func (p TodosProvider) Update(msg tea.Msg) (TodosProvider, tea.Cmd) {
	switch msg := msg.(type) {
	case todosLoadedMsg:
		p.todos = msg.todos
		return p, p.emitState()

	case requestTodosStateMsg:
		return p, p.emitState()

	case createTodoIntentMsg:
		return p, createTodo(msg.name, msg.description, msg.workspaceID)

	case todoCreatedMsg:
		return p, fetchTodos()

	case deleteTodoIntentMsg:
		return p, deleteTodo(msg.id)

	case todoDeletedMsg:
		return p, fetchTodos()
	}

	return p, nil
}
