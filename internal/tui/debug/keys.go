package debug

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Back key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Back}}
}

var keys = keyMap{
	Back: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}
