package bundlepicker

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Select key.Binding
	Toggle key.Binding
	Filter key.Binding
	Back   key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.Toggle, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Select, k.Toggle, k.Filter, k.Back}}
}

var Keys = keyMap{
	Select: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
	Toggle: key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
	Filter: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

func (m Model) Keys() help.KeyMap {
	return Keys
}
