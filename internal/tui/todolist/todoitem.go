package todolist

import "github.com/eleonorayaya/utena/internal/todo"

type todoItem struct {
	todo todo.Todo
}

func (i todoItem) Title() string { return i.todo.Name }
func (i todoItem) Description() string {
	var desc string
	if i.todo.Workspace != nil {
		desc = i.todo.Workspace.Name
	}
	if desc == "" {
		desc = "no workspace"
	}
	if i.todo.Description != "" {
		desc += " · " + i.todo.Description
	}
	return desc
}
func (i todoItem) FilterValue() string { return i.todo.Name }
