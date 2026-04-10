package workspacelist

import (
	"github.com/eleonorayaya/utena/internal/tui/workspacepicker"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type workspaceItem struct {
	workspace workspace.Workspace
}

func (i workspaceItem) Title() string {
	title := i.workspace.Name
	if i.workspace.IsGitRepo {
		title += " (git)"
	}
	if i.workspace.IsHidden {
		title += " [hidden]"
	}
	return title
}

func (i workspaceItem) Description() string {
	return workspacepicker.AbbreviatePath(i.workspace.Path)
}

func (i workspaceItem) FilterValue() string {
	return i.workspace.Name
}
