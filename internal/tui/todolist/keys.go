package todolist

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Delete    key.Binding
	ToggleAll key.Binding
	New       key.Binding
	Back      key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Delete, k.ToggleAll, k.New, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Delete, k.ToggleAll, k.New, k.Back}}
}

var keys = keyMap{
	Delete:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	ToggleAll: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all/current")),
	New:       key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	Back:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

var _ help.KeyMap = keys
