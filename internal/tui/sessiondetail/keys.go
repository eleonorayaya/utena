package sessiondetail

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Activate      key.Binding
	Open          key.Binding
	Repair        key.Binding
	Archive       key.Binding
	Delete        key.Binding
	AddWorkspace  key.Binding
	Bundles       key.Binding
	Up            key.Binding
	Down          key.Binding
	NextSection   key.Binding
	PrevSection   key.Binding
	Block         key.Binding
	Resolve       key.Binding
	RemoveBlocker key.Binding
	Back          key.Binding
	showOpen      bool
	showNext      bool
}

func (k keyMap) ShortHelp() []key.Binding {
	out := []key.Binding{k.Activate}
	if k.showOpen {
		out = append(out, k.Open)
	}
	if k.showNext {
		out = append(out, k.Down)
	}
	return append(out, k.Block, k.Archive, k.Back)
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Activate, k.Open, k.Back},
		{k.Up, k.Down, k.NextSection, k.PrevSection},
		{k.Block, k.Resolve, k.RemoveBlocker},
		{k.Archive, k.Delete, k.Repair, k.AddWorkspace, k.Bundles},
	}
}

var keys = keyMap{
	Activate:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "switch")),
	Open:          key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open tab/PR")),
	Repair:        key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "repair")),
	Archive:       key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "archive")),
	Delete:        key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	AddWorkspace:  key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "add workspace")),
	Bundles:       key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "plugins")),
	Up:            key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "prev item")),
	Down:          key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "next item")),
	NextSection:   key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "next section")),
	PrevSection:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "prev section")),
	Block:         key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "block")),
	Resolve:       key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "resolve blocker")),
	RemoveBlocker: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "delete blocker")),
	Back:          key.NewBinding(key.WithKeys("tab", "shift+tab", "esc"), key.WithHelp("tab/esc", "sessions")),
}

var _ help.KeyMap = keys

var (
	openTabKey = key.NewBinding(key.WithKeys("enter", "o"), key.WithHelp("enter/o", "open tab"))
	openPRKey  = key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open PR"))
)
