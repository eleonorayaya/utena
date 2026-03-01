package sessionform

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type mergedKeyMap struct {
	keymaps []help.KeyMap
}

func (m mergedKeyMap) ShortHelp() []key.Binding {
	var bindings []key.Binding
	for _, km := range m.keymaps {
		bindings = append(bindings, km.ShortHelp()...)
	}
	return bindings
}

func (m mergedKeyMap) FullHelp() [][]key.Binding {
	var groups [][]key.Binding
	for _, km := range m.keymaps {
		groups = append(groups, km.FullHelp()...)
	}
	return groups
}

type formKeyMap struct {
	Submit key.Binding
	Back   key.Binding
}

func (k formKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.Back}
}

func (k formKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Submit, k.Back}}
}

var formKeys = formKeyMap{
	Submit: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "create")),
	Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}
