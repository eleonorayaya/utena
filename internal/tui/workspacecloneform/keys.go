package workspacecloneform

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Submit    key.Binding
	NextField key.Binding
	PrevField key.Binding
	NextRoot  key.Binding
	PrevRoot  key.Binding
	Back      key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.NextField, k.PrevField, k.NextRoot, k.PrevRoot, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Submit, k.NextField, k.PrevField, k.NextRoot, k.PrevRoot, k.Back}}
}

var keys = keyMap{
	Submit:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "clone")),
	NextField: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
	PrevField: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev field")),
	NextRoot:  key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "next root")),
	PrevRoot:  key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "prev root")),
	Back:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

var _ help.KeyMap = keys
