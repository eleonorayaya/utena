package workspacedetail

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	PRs     key.Binding
	Migrate key.Binding
	Back    key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.PRs, k.Migrate, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.PRs, k.Migrate, k.Back}}
}

var keys = keyMap{
	PRs:     key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pull requests")),
	Migrate: key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "migrate to bare")),
	Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

var _ help.KeyMap = keys
