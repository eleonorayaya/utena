package branchpicker

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Select key.Binding
	Fetch  key.Binding
	Manual key.Binding
	Back   key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.Fetch, k.Manual, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Select, k.Fetch, k.Manual, k.Back}}
}

var Keys = keyMap{
	Select: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Fetch:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Manual: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new name")),
	Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}
