package filepicker

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	SelectDir    key.Binding
	ToggleHidden key.Binding
	Back         key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.SelectDir, k.ToggleHidden, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.SelectDir, k.ToggleHidden, k.Back}}
}

var Keys = keyMap{
	SelectDir:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "select dir")),
	ToggleHidden: key.NewBinding(key.WithKeys("."), key.WithHelp(".", "hidden")),
	Back:         key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

var _ help.KeyMap = Keys
