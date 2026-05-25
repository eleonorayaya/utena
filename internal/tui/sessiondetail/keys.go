package sessiondetail

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Activate     key.Binding
	Repair       key.Binding
	Archive      key.Binding
	Delete       key.Binding
	AddWorkspace key.Binding
	Back         key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Activate, k.Repair, k.Archive, k.Delete, k.AddWorkspace, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Activate, k.Repair, k.Archive, k.Delete, k.AddWorkspace, k.Back}}
}

var keys = keyMap{
	Activate:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "activate")),
	Repair:       key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "repair")),
	Archive:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "archive")),
	Delete:       key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	AddWorkspace: key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "add workspace")),
	Back:         key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

var _ help.KeyMap = keys
