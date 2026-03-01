package workspacepicker

import (
	"os"
	"strings"

	"github.com/eleonorayaya/utena/internal/workspace"
)

type workspaceItem struct {
	workspace workspace.Workspace
}

func (i workspaceItem) Title() string       { return i.workspace.Name }
func (i workspaceItem) Description() string { return AbbreviatePath(i.workspace.Path) }
func (i workspaceItem) FilterValue() string { return i.workspace.Name }

func AbbreviatePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
