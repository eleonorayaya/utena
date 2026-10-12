package blockerform

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Kind   key.Binding
	Next   key.Binding
	Prev   key.Binding
	Submit key.Binding
	Back   key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Kind, k.Next, k.Submit, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Kind, k.Next, k.Prev, k.Submit, k.Back}}
}

var keys = keyMap{
	Kind:   key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "kind")),
	Next:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
	Prev:   key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev field")),
	Submit: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save")),
	Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}

var _ help.KeyMap = keys
