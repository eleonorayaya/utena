package addworkspaceform

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Next   key.Binding
	Prev   key.Binding
	Edit   key.Binding
	Add    key.Binding
	Cancel key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	if k.Add.Enabled() {
		return []key.Binding{k.Add, k.Prev, k.Cancel}
	}
	return []key.Binding{k.Next, k.Edit, k.Cancel}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Next, k.Prev, k.Edit, k.Add, k.Cancel}}
}

var formKeys = keyMap{
	Next:   key.NewBinding(key.WithKeys("tab", "down"), key.WithHelp("tab", "next")),
	Prev:   key.NewBinding(key.WithKeys("shift+tab", "up"), key.WithHelp("shift+tab", "previous")),
	Edit:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit")),
	Add:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "add")),
	Cancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}

var _ help.KeyMap = formKeys
