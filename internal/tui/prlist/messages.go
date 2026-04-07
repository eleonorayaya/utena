package prlist

import tea "github.com/charmbracelet/bubbletea"

type SelectMsg struct {
	WorkspaceID uint
}

func Select(workspaceID uint) tea.Cmd {
	return func() tea.Msg { return SelectMsg{WorkspaceID: workspaceID} }
}
