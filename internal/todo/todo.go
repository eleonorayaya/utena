package todo

import (
	"errors"
	"time"
)

var ErrTodoNotFound = errors.New("todo not found")

type Todo struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	WorkspaceID   string    `json:"workspace_id,omitempty"`
	WorkspaceName string    `json:"workspace_name,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}
