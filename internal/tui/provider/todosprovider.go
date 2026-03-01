package provider

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/todo"
)

type TodosStateUpdatedMsg struct {
	Todos []todo.Todo
}

type TodoCreatedMsg struct{}

func FetchTodos() tea.Cmd {
	return func() tea.Msg { return fetchTodosIntentMsg{} }
}

func RequestTodosState() tea.Cmd {
	return func() tea.Msg { return requestTodosStateMsg{} }
}

func CreateTodo(name, description, workspaceID string) tea.Cmd {
	return func() tea.Msg {
		return createTodoIntentMsg{
			name:        name,
			description: description,
			workspaceID: workspaceID,
		}
	}
}

func DeleteTodo(id string) tea.Cmd {
	return func() tea.Msg { return deleteTodoIntentMsg{id: id} }
}

type fetchTodosIntentMsg struct{}
type requestTodosStateMsg struct{}

type createTodoIntentMsg struct {
	name        string
	description string
	workspaceID string
}

type deleteTodoIntentMsg struct {
	id string
}

type todosLoadedMsg struct {
	todos []todo.Todo
}

type todoDeletedMsg struct {
	id string
}

type todosProvider struct {
	client *client
	todos  []todo.Todo
}

func newTodosProvider(c *client) todosProvider {
	return todosProvider{client: c}
}

func (p todosProvider) emitState() tea.Cmd {
	todos := p.todos
	return func() tea.Msg {
		return TodosStateUpdatedMsg{Todos: todos}
	}
}

func (p todosProvider) Update(msg tea.Msg) (todosProvider, tea.Cmd) {
	switch msg := msg.(type) {
	case todosLoadedMsg:
		p.todos = msg.todos
		return p, p.emitState()

	case fetchTodosIntentMsg:
		return p, p.client.fetchTodos()

	case requestTodosStateMsg:
		return p, p.emitState()

	case createTodoIntentMsg:
		return p, p.client.createTodo(msg.name, msg.description, msg.workspaceID)

	case TodoCreatedMsg:
		return p, p.client.fetchTodos()

	case deleteTodoIntentMsg:
		return p, p.client.deleteTodo(msg.id)

	case todoDeletedMsg:
		return p, p.client.fetchTodos()
	}

	return p, nil
}
