package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type view int

const (
	sessionListView view = iota
	sessionFormView
	TodoListView
	todoFormView
	debugView
	backView view = -1
)

type routerKeyMap struct {
	Debug key.Binding
}

func (k routerKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Debug}
}

func (k routerKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Debug}}
}

var routerKeys = routerKeyMap{
	Debug: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "debug")),
}

type Router struct {
	sessionList  SessionListModel
	sessionForm  SessionFormModel
	todoList     TodoListModel
	todoForm     TodoFormModel
	debug        DebugModel
	activeView   view
	previousView view
}

func NewRouter(logPath string) Router {
	return Router{
		activeView:  sessionListView,
		sessionList: NewSessionListModel(),
		sessionForm: NewSessionFormModel(),
		todoList:    NewTodoListModel(),
		todoForm:    NewTodoFormModel(),
		debug:       NewDebugModel(logPath),
	}
}

func (r Router) Init() (Router, tea.Cmd) {
	var cmd tea.Cmd
	switch r.activeView {
	case sessionListView:
		r.sessionList, cmd = r.sessionList.Init()
	case sessionFormView:
		r.sessionForm, cmd = r.sessionForm.Init()
	case TodoListView:
		r.todoList, cmd = r.todoList.Init()
	case todoFormView:
		r.todoForm, cmd = r.todoForm.Init()
	case debugView:
		r.debug, cmd = r.debug.Init()
	}
	return r, cmd
}

func (r Router) Update(msg tea.Msg) (Router, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return r.OnWindowSizeMsg(msg)
	case tea.KeyMsg:
		r, cmd, handled := r.OnKeyMsg(msg)
		if handled {
			return r, cmd
		}
	case navigateMsg:
		return r.onNavigate(msg)
	}
	return r.routeToActiveView(msg)
}

func (r Router) OnKeyMsg(msg tea.KeyMsg) (Router, tea.Cmd, bool) {
	if key.Matches(msg, routerKeys.Debug) && r.activeView != debugView {
		r.previousView = r.activeView
		r.activeView = debugView
		return r, nil, true
	}
	return r, nil, false
}

func (r Router) routeToActiveView(msg tea.Msg) (Router, tea.Cmd) {
	var cmd tea.Cmd
	switch r.activeView {
	case sessionListView:
		r.sessionList, cmd = r.sessionList.Update(msg)
	case sessionFormView:
		r.sessionForm, cmd = r.sessionForm.Update(msg)
	case TodoListView:
		r.todoList, cmd = r.todoList.Update(msg)
	case todoFormView:
		r.todoForm, cmd = r.todoForm.Update(msg)
	case debugView:
		r.debug, cmd = r.debug.Update(msg)
	}
	return r, cmd
}

func (r Router) onNavigate(msg navigateMsg) (Router, tea.Cmd) {
	if msg.target == backView {
		r.activeView = r.previousView
		return r, nil
	}
	r.previousView = r.activeView
	r.activeView = msg.target
	return r.Init()
}

func (r Router) OnWindowSizeMsg(msg tea.WindowSizeMsg) (Router, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	r.sessionList, cmd = r.sessionList.Update(msg)
	cmds = append(cmds, cmd)
	r.sessionForm, cmd = r.sessionForm.Update(msg)
	cmds = append(cmds, cmd)
	r.todoList, cmd = r.todoList.Update(msg)
	cmds = append(cmds, cmd)
	r.todoForm, cmd = r.todoForm.Update(msg)
	cmds = append(cmds, cmd)
	return r, tea.Batch(cmds...)
}

func (r Router) View() string {
	switch r.activeView {
	case sessionFormView:
		return r.sessionForm.View()
	case TodoListView:
		return r.todoList.View()
	case todoFormView:
		return r.todoForm.View()
	case debugView:
		return r.debug.View()
	default:
		return r.sessionList.View()
	}
}

func (r Router) Keys() help.KeyMap {
	keymaps := []help.KeyMap{routerKeys}

	switch r.activeView {
	case sessionListView:
		keymaps = append(keymaps, r.sessionList.Keys())
	case sessionFormView:
		keymaps = append(keymaps, r.sessionForm.Keys())
	case TodoListView:
		keymaps = append(keymaps, r.todoList.Keys())
	case todoFormView:
		keymaps = append(keymaps, r.todoForm.Keys())
	case debugView:
		keymaps = append(keymaps, r.debug.Keys())
	}

	return mergedKeyMap{keymaps: keymaps}
}
