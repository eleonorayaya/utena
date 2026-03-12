package todo

import (
	"errors"
	"net/http"
)

type CreateTodoRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	WorkspaceID *uint  `json:"workspace_id"`
}

func (c *CreateTodoRequest) Bind(r *http.Request) error {
	if c.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

type TodoResponse struct {
	*Todo
	WorkspaceName string `json:"workspace_name,omitempty"`
}

func NewTodoResponse(t *Todo) *TodoResponse {
	resp := &TodoResponse{Todo: t}
	if t.Workspace != nil {
		resp.WorkspaceName = t.Workspace.Name
	}
	return resp
}

func (r *TodoResponse) Render(w http.ResponseWriter, req *http.Request) error {
	return nil
}

type TodoListResponse struct {
	Todos []*TodoResponse `json:"todos"`
}

func NewTodoListResponse(todos []Todo) *TodoListResponse {
	resp := make([]*TodoResponse, len(todos))
	for i := range todos {
		resp[i] = NewTodoResponse(&todos[i])
	}
	return &TodoListResponse{Todos: resp}
}

func (r *TodoListResponse) Render(w http.ResponseWriter, req *http.Request) error {
	return nil
}
