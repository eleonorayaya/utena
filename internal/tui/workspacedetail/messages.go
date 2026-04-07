package workspacedetail

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type SelectMsg struct {
	Workspace workspace.Workspace
}

func Select(ws workspace.Workspace) tea.Cmd {
	return func() tea.Msg { return SelectMsg{Workspace: ws} }
}
