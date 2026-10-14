package workspacepicker

import "github.com/eleonorayaya/utena/internal/workspace"

type SelectedMsg struct {
	Workspaces []workspace.Workspace
}

type AddDirectoryMsg struct{}

type ClosedMsg struct{}
