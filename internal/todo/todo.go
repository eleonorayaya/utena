package todo

import (
	"errors"

	"github.com/eleonorayaya/utena/internal/workspace"
	"gorm.io/gorm"
)

var ErrTodoNotFound = errors.New("todo not found")

type Todo struct {
	gorm.Model
	Name          string               `json:"name"`
	Description   string               `json:"description"`
	WorkspaceID   *uint                `json:"workspace_id,omitempty" gorm:"index"`
	WorkspaceName string               `json:"workspace_name,omitempty" gorm:"-"`
	Workspace     *workspace.Workspace `json:"-" gorm:"foreignKey:WorkspaceID"`
}
