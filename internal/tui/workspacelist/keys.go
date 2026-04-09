package workspacelist

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Select key.Binding
	Delete key.Binding
	Back   key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.Delete, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Select, k.Delete, k.Back}}
}

var keys = keyMap{
	Select: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Delete: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

var _ help.KeyMap = keys
