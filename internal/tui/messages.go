package tui

import "github.com/eleonorayaya/utena/internal/workspace"

type navigateMsg struct {
	target view
}

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
