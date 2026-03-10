package todo

import (
	"errors"
	"time"

	"github.com/eleonorayaya/utena/internal/workspace"
)

var ErrTodoNotFound = errors.New("todo not found")

type Todo struct {
	ID            string               `json:"id" gorm:"primaryKey"`
	Name          string               `json:"name"`
	Description   string               `json:"description"`
	WorkspaceID   string               `json:"workspace_id,omitempty" gorm:"index"`
	WorkspaceName string               `json:"workspace_name,omitempty" gorm:"-"`
	CreatedAt     time.Time            `json:"created_at"`
	Workspace     *workspace.Workspace `json:"-" gorm:"foreignKey:WorkspaceID"`
}
