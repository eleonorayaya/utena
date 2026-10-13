package tui

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

type appKeyMap struct {
	Quit key.Binding
}

func (k appKeyMap) ShortHelp() []key.Binding {
	return nil
}

func (k appKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Quit}}
}

var appKeys = appKeyMap{
	Quit: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
}
