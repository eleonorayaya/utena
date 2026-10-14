package workspacecloneform

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Submit    key.Binding
	NextField key.Binding
	PrevField key.Binding
	Root      key.Binding
	Back      key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.NextField, k.Root, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Submit, k.NextField, k.PrevField, k.Root, k.Back}}
}

var keys = keyMap{
	Submit:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "clone")),
	NextField: key.NewBinding(key.WithKeys("tab", "down"), key.WithHelp("tab", "next")),
	PrevField: key.NewBinding(key.WithKeys("shift+tab", "up"), key.WithHelp("shift+tab", "previous")),
	Root:      key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "root")),
	Back:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

var _ help.KeyMap = keys
