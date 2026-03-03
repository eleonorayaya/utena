package statusview

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Select   key.Binding
	Collapse key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Select, k.Collapse}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Select, k.Collapse}}
}

var keys = keyMap{
	Up:       key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("↑/k", "up")),
	Down:     key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("↓/j", "down")),
	Select:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "switch")),
	Collapse: key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc", "collapse")),
}

var _ help.KeyMap = keys
