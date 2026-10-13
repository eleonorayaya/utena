package sessiondetail

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Activate      key.Binding
	OpenPR        key.Binding
	Repair        key.Binding
	Archive       key.Binding
	Delete        key.Binding
	AddWorkspace  key.Binding
	Up            key.Binding
	Down          key.Binding
	Block         key.Binding
	Resolve       key.Binding
	RemoveBlocker key.Binding
	Back          key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Activate, k.OpenPR, k.Block, k.Archive, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Activate, k.OpenPR, k.Back},
		{k.Block, k.Up, k.Down, k.Resolve, k.RemoveBlocker},
		{k.Archive, k.Delete, k.Repair, k.AddWorkspace},
	}
}

var keys = keyMap{
	Activate:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "switch")),
	OpenPR:        key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open PR")),
	Repair:        key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "repair")),
	Archive:       key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "archive")),
	Delete:        key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	AddWorkspace:  key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "add workspace")),
	Up:            key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "prev blocker")),
	Down:          key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "next blocker")),
	Block:         key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "block")),
	Resolve:       key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "resolve blocker")),
	RemoveBlocker: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "delete blocker")),
	Back:          key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

var _ help.KeyMap = keys
