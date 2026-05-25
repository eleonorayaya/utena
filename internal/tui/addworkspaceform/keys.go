package addworkspaceform

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/eleonorayaya/utena/internal/tui/branchpicker"
	"github.com/eleonorayaya/utena/internal/tui/workspacepicker"
)

type formKeyMap struct {
	Back key.Binding
}

func (k formKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Back}
}

func (k formKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Back}}
}

var formKeys = formKeyMap{
	Back: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

type mergedKeyMap struct {
	keymaps []help.KeyMap
}

func (m mergedKeyMap) ShortHelp() []key.Binding {
	var out []key.Binding
	for _, km := range m.keymaps {
		out = append(out, km.ShortHelp()...)
	}
	return out
}

func (m mergedKeyMap) FullHelp() [][]key.Binding {
	var out [][]key.Binding
	for _, km := range m.keymaps {
		out = append(out, km.FullHelp()...)
	}
	return out
}

var _ help.KeyMap = formKeys
var _ help.KeyMap = mergedKeyMap{keymaps: []help.KeyMap{formKeys, workspacepicker.Keys, branchpicker.Keys}}
