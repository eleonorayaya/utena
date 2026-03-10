package sessionprogress

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Back   key.Binding
	Repair key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Repair, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Repair, k.Back}}
}

var keys = keyMap{
	Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Repair: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "retry")),
}
