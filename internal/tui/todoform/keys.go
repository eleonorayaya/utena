package todoform

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

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

type inputKeyMap struct {
	Submit  key.Binding
	NextTab key.Binding
	Back    key.Binding
}

func (k inputKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.NextTab, k.Back}
}

func (k inputKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Submit, k.NextTab, k.Back}}
}

var inputKeys = inputKeyMap{
	Submit:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "create")),
	NextTab: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
	Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

var _ help.KeyMap = formKeys
var _ help.KeyMap = inputKeys
