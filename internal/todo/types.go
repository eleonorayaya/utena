package todo

import (
	"errors"
	"net/http"
)

type CreateTodoRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	WorkspaceID string `json:"workspace_id"`
}

func (c *CreateTodoRequest) Bind(r *http.Request) error {
	if c.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

type TodoResponse struct {
	*Todo
}

func NewTodoResponse(t *Todo) *TodoResponse {
	return &TodoResponse{Todo: t}
}

func (r *TodoResponse) Render(w http.ResponseWriter, req *http.Request) error {
	return nil
}

type TodoListResponse struct {
	Todos []Todo `json:"todos"`
}

func NewTodoListResponse(todos []Todo) *TodoListResponse {
	return &TodoListResponse{Todos: todos}
}

func (r *TodoListResponse) Render(w http.ResponseWriter, req *http.Request) error {
	return nil
}
