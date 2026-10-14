package blockerform

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Next   key.Binding
	Prev   key.Binding
	Kind   key.Binding
	Submit key.Binding
	Back   key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.Next, k.Kind, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Next, k.Prev, k.Kind, k.Submit, k.Back}}
}

var keys = keyMap{
	Next:   key.NewBinding(key.WithKeys("tab", "down"), key.WithHelp("tab", "next")),
	Prev:   key.NewBinding(key.WithKeys("shift+tab", "up"), key.WithHelp("shift+tab", "previous")),
	Kind:   key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "kind")),
	Submit: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save")),
	Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}

var _ help.KeyMap = keys
