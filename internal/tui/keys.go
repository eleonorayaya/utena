package tui

import "github.com/charmbracelet/bubbles/key"

type sessionListKeys struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	New    key.Binding
	Quit   key.Binding
}

func (k sessionListKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Select, k.New, k.Quit}
}

func (k sessionListKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Select, k.New, k.Quit}}
}

type workspacePickerKeys struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Back   key.Binding
}

func (k workspacePickerKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Select, k.Back}
}

func (k workspacePickerKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Select, k.Back}}
}

type nameInputKeys struct {
	Confirm key.Binding
	Back    key.Binding
}

func (k nameInputKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Confirm, k.Back}
}

func (k nameInputKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Confirm, k.Back}}
}

var (
	sessionListKeyMap = sessionListKeys{
		Up:     key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:   key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Select: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		New:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		Quit:   key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q", "quit")),
	}

	workspacePickerKeyMap = workspacePickerKeys{
		Up:     key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:   key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Select: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}

	nameInputKeyMap = nameInputKeys{
		Confirm: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "create")),
		Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
)
