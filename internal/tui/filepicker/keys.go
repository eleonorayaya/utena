package filepicker

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Open         key.Binding
	Up           key.Binding
	SelectDir    key.Binding
	ToggleHidden key.Binding
	Filter       key.Binding
	Back         key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Open, k.Up, k.SelectDir, k.ToggleHidden, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Open, k.Up, k.SelectDir, k.ToggleHidden, k.Filter, k.Back}}
}

var Keys = keyMap{
	Open:         key.NewBinding(key.WithKeys("enter", "right", "l"), key.WithHelp("enter", "open")),
	Up:           key.NewBinding(key.WithKeys("left", "h", "backspace"), key.WithHelp("←", "parent")),
	SelectDir:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "select dir")),
	ToggleHidden: key.NewBinding(key.WithKeys("."), key.WithHelp(".", "hidden")),
	Filter:       key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	Back:         key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

var _ help.KeyMap = Keys
