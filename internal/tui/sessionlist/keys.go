package sessionlist

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Up           key.Binding
	Down         key.Binding
	Select       key.Binding
	OpenPR       key.Binding
	Archive      key.Binding
	Delete       key.Binding
	Block        key.Binding
	New          key.Binding
	Info         key.Binding
	Workspaces   key.Binding
	ToggleHidden key.Binding
	Quit         key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.OpenPR, k.New, k.Block, k.Archive}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Select, k.Info, k.OpenPR},
		{k.New, k.Block, k.Archive, k.Delete},
		{k.Workspaces, k.ToggleHidden, k.Quit},
	}
}

var keys = keyMap{
	Up:           key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:         key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Select:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "switch")),
	OpenPR:       key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open PR")),
	Archive:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "archive")),
	Delete:       key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	Block:        key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "block")),
	New:          key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	Info:         key.NewBinding(key.WithKeys("i", "right"), key.WithHelp("i/→", "details")),
	Workspaces:   key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "workspaces")),
	ToggleHidden: key.NewBinding(key.WithKeys("."), key.WithHelp(".", "hidden")),
	Quit:         key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
}

var _ help.KeyMap = keys
