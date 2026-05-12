package workspacepicker

import "github.com/eleonorayaya/utena/internal/workspace"

type SelectedMsg struct {
	Workspace  workspace.Workspace
	Workspaces []workspace.Workspace
}

type AddDirectoryMsg struct{}
