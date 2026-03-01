package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type mergedKeyMap struct {
	keymaps []help.KeyMap
}

func (m mergedKeyMap) ShortHelp() []key.Binding {
	var bindings []key.Binding
	for _, km := range m.keymaps {
		bindings = append(bindings, km.ShortHelp()...)
	}
	return bindings
}

func (m mergedKeyMap) FullHelp() [][]key.Binding {
	var groups [][]key.Binding
	for _, km := range m.keymaps {
		groups = append(groups, km.FullHelp()...)
	}
	return groups
}

type appKeyMap struct {
	Quit  key.Binding
	Debug key.Binding
}

func (k appKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Quit, k.Debug}
}

func (k appKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Quit, k.Debug}}
}

var appKeys = appKeyMap{
	Quit:  key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	Debug: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "debug")),
}

type sessionListKeyMap struct {
	Select key.Binding
	Close  key.Binding
	New    key.Binding
	Todos  key.Binding
}

func (k sessionListKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.Close, k.New, k.Todos}
}

func (k sessionListKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Select, k.Close, k.New, k.Todos}}
}

var sessionListKeys = sessionListKeyMap{
	Select: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Close:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "close")),
	New:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	Todos:  key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "todos")),
}

type todoListKeyMap struct {
	Delete    key.Binding
	ToggleAll key.Binding
	New       key.Binding
	Back      key.Binding
}

func (k todoListKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Delete, k.ToggleAll, k.New, k.Back}
}

func (k todoListKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Delete, k.ToggleAll, k.New, k.Back}}
}

var todoListKeys = todoListKeyMap{
	Delete:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	ToggleAll: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all/current")),
	New:       key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	Back:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

type workspacePickerKeyMap struct {
	Select key.Binding
	AddDir key.Binding
	Back   key.Binding
}

func (k workspacePickerKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.AddDir, k.Back}
}

func (k workspacePickerKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Select, k.AddDir, k.Back}}
}

var workspacePickerKeys = workspacePickerKeyMap{
	Select: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	AddDir: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add dir")),
	Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

type branchPickerKeyMap struct {
	Select key.Binding
	Back   key.Binding
}

func (k branchPickerKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.Back}
}

func (k branchPickerKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Select, k.Back}}
}

var branchPickerKeys = branchPickerKeyMap{
	Select: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

type filePickerKeyMap struct {
	SelectDir    key.Binding
	ToggleHidden key.Binding
	Back         key.Binding
}

func (k filePickerKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.SelectDir, k.ToggleHidden, k.Back}
}

func (k filePickerKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.SelectDir, k.ToggleHidden, k.Back}}
}

var filePickerKeys = filePickerKeyMap{
	SelectDir:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "select dir")),
	ToggleHidden: key.NewBinding(key.WithKeys("."), key.WithHelp(".", "hidden")),
	Back:         key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

type formKeyMap struct {
	Submit key.Binding
	Back   key.Binding
}

func (k formKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.Back}
}

func (k formKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Submit, k.Back}}
}

var formKeys = formKeyMap{
	Submit: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "create")),
	Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

type todoFormInputKeyMap struct {
	Submit  key.Binding
	NextTab key.Binding
	Back    key.Binding
}

func (k todoFormInputKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.NextTab, k.Back}
}

func (k todoFormInputKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Submit, k.NextTab, k.Back}}
}

var todoFormInputKeys = todoFormInputKeyMap{
	Submit:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "create")),
	NextTab: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
	Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}
