package router

import tea "github.com/charmbracelet/bubbletea"

type NavigateToMsg struct {
	Target View
}

type BackMsg struct{}

func NavigateTo(target View) tea.Cmd {
	return func() tea.Msg { return NavigateToMsg{Target: target} }
}

func Back() tea.Cmd {
	return func() tea.Msg { return BackMsg{} }
}

type SetHelpVisibleMsg struct{ Visible bool }

func SetHelpVisible(visible bool) tea.Cmd {
	return func() tea.Msg { return SetHelpVisibleMsg{Visible: visible} }
}
