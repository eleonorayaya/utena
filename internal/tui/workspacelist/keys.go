package workspacelist

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Select       key.Binding
	CloneFromURL key.Binding
	Delete       key.Binding
	Hide         key.Binding
	ToggleHidden key.Binding
	Back         key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.CloneFromURL, k.Hide, k.ToggleHidden, k.Delete, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Select, k.CloneFromURL, k.Hide, k.ToggleHidden, k.Delete, k.Back}}
}

var keys = keyMap{
	Select:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	CloneFromURL: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clone from URL")),
	Delete:       key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	Hide:         key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "hide")),
	ToggleHidden: key.NewBinding(key.WithKeys("."), key.WithHelp(".", "show hidden")),
	Back:         key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

var _ help.KeyMap = keys
