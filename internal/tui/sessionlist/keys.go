package sessionlist

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Select key.Binding
	Close  key.Binding
	New    key.Binding
	Todos  key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.Close, k.New, k.Todos}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Select, k.Close, k.New, k.Todos}}
}

var keys = keyMap{
	Select: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Close:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "close")),
	New:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	Todos:  key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "todos")),
}

var _ help.KeyMap = keys
