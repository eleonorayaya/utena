package tui

import "github.com/charmbracelet/bubbles/key"

var (
	selectKey     = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select"))
	newSessionKey = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new"))
	backKey       = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
	debugKey      = key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "debug"))
)

type nameInputKeys struct {
	Confirm key.Binding
	Back    key.Binding
}

func (k nameInputKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Confirm, k.Back}
}

func (k nameInputKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Confirm, k.Back}}
}

var nameInputKeyMap = nameInputKeys{
	Confirm: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "create")),
	Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}
