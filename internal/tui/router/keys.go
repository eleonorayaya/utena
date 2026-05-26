package router

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type routerKeyMap struct {
	Debug key.Binding
}

func (k routerKeyMap) ShortHelp() []key.Binding {
	return nil
}

func (k routerKeyMap) FullHelp() [][]key.Binding {
	return nil
}

var routerKeys = routerKeyMap{
	Debug: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "debug")),
}

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
