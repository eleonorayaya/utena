package workspacelist

import (
	"os"
	"path/filepath"
	"strings"

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
	return title
}

func (i workspaceItem) Description() string {
	return abbreviatePath(i.workspace.Path)
}

func (i workspaceItem) FilterValue() string {
	return i.workspace.Name
}

func abbreviatePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return filepath.Join("~", path[len(home):])
	}
	return path
}
