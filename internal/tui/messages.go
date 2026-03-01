package tui

import (
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/todo"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type sessionsStateUpdatedMsg struct {
	sessions       []session.Session
	claudeSessions map[string][]claude.ClaudeSession
}

type workspacesStateUpdatedMsg struct {
	workspaces        []workspace.Workspace
	activeWorkspaceID string
}

type todosStateUpdatedMsg struct {
	todos []todo.Todo
}

type requestSessionsStateMsg struct{}
type requestWorkspacesStateMsg struct{}
type requestTodosStateMsg struct{}

type setActiveWorkspaceMsg struct {
	workspaceID string
}

type activateSessionMsg struct {
	name string
}

type createSessionIntentMsg struct {
	name          string
	workspaceID   string
	branch        string
	workspacePath string
}

type deleteSessionIntentMsg struct {
	id string
}

type createTodoIntentMsg struct {
	name        string
	description string
	workspaceID string
}

type deleteTodoIntentMsg struct {
	id string
}

type addWorkspaceIntentMsg struct {
	path   string
	asRoot bool
}

type sessionFormCancelledMsg struct{}
type todoFormCancelledMsg struct{}

type openSessionFormMsg struct{}
type openTodosViewMsg struct{}
type openTodoFormMsg struct{}
type returnToSessionsMsg struct{}

type workspaceSelectedMsg struct {
	workspace workspace.Workspace
}

type branchSelectedMsg struct {
	branch string
}

type addDirectoryRequestMsg struct{}

type directorySelectedMsg struct {
	path string
}
