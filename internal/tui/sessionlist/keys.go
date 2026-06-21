package sessionlist

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Up             key.Binding
	Down           key.Binding
	Select         key.Binding
	Close          key.Binding
	New            key.Binding
	Info           key.Binding
	Todos          key.Binding
	Workspaces     key.Binding
	ToggleBroken   key.Binding
	ToggleArchived key.Binding
	Quit           key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.Close, k.New, k.Info, k.Todos, k.Workspaces, k.ToggleBroken, k.ToggleArchived, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Select, k.Close, k.New, k.Info, k.Todos, k.Workspaces, k.ToggleBroken, k.ToggleArchived, k.Quit}}
}

var keys = keyMap{
	Up:             key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:           key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Select:         key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Close:          key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "archive/delete")),
	New:            key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	Info:           key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "info")),
	Todos:          key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "todos")),
	Workspaces:     key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "workspaces")),
	ToggleBroken:   key.NewBinding(key.WithKeys("."), key.WithHelp(".", "toggle broken")),
	ToggleArchived: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "toggle archived")),
	Quit:           key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
}

var _ help.KeyMap = keys
