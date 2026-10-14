package workspacepicker

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Select       key.Binding
	Toggle       key.Binding
	ToggleHidden key.Binding
	AddDir       key.Binding
	Filter       key.Binding
	Back         key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.Toggle, k.AddDir, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Select, k.Toggle, k.ToggleHidden, k.AddDir, k.Filter, k.Back}}
}

var Keys = keyMap{
	Select:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
	Toggle:       key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
	ToggleHidden: key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "show hidden")),
	AddDir:       key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add dir")),
	Filter:       key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	Back:         key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

func (m Model) Keys() help.KeyMap {
	k := Keys
	k.Toggle.SetEnabled(m.multi)
	k.AddDir.SetEnabled(!m.noAddDir)
	return k
}
