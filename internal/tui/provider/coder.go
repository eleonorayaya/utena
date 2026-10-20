package provider

import (
	tea "github.com/charmbracelet/bubbletea"
)

type CoderStatusLoadedMsg struct {
	Configured bool
	Err        error
}

type fetchCoderStatusIntentMsg struct{}

func FetchCoderStatus() tea.Cmd {
	return func() tea.Msg { return fetchCoderStatusIntentMsg{} }
}
