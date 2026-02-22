package tui

import "github.com/charmbracelet/bubbles/key"

var (
	selectKey       = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select"))
	newSessionKey   = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new"))
	closeSessionKey = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "close"))
	backKey         = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
	debugKey        = key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "debug"))
	addWorkspaceKey = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add dir"))
	selectDirKey    = key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "select dir"))
	toggleHiddenKey = key.NewBinding(key.WithKeys("."), key.WithHelp(".", "hidden"))
	todoKey         = key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "todos"))
	newTodoKey      = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new"))
	deleteTodoKey   = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete"))
	toggleAllKey    = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all/current"))
)

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

var nameInputKeyMap = nameInputKeys{
	Confirm: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "create")),
	Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}
