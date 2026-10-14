package workspacelist

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	CloneFromURL key.Binding
	Migrate      key.Binding
	Hide         key.Binding
	Delete       key.Binding
	ToggleHidden key.Binding
	Filter       key.Binding
	Back         key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.CloneFromURL, k.Migrate, k.Hide, k.Delete}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.CloneFromURL, k.Migrate, k.Hide, k.Delete, k.ToggleHidden, k.Filter, k.Back}}
}

var keys = keyMap{
	CloneFromURL: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clone")),
	Migrate:      key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "migrate")),
	Hide:         key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "hide")),
	Delete:       key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	ToggleHidden: key.NewBinding(key.WithKeys("."), key.WithHelp(".", "show hidden")),
	Filter:       key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	Back:         key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

var _ help.KeyMap = keys
