package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/debug"
	"github.com/eleonorayaya/utena/internal/tui/sessionform"
	"github.com/eleonorayaya/utena/internal/tui/sessionlist"
	"github.com/eleonorayaya/utena/internal/tui/todoform"
	"github.com/eleonorayaya/utena/internal/tui/todolist"
)

type view int

const (
	sessionListView view = iota
	sessionFormView
	TodoListView
	todoFormView
	debugView
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
	sessionList  sessionlist.Model
	sessionForm  sessionform.Model
	todoList     todolist.Model
	todoForm     todoform.Model
	debug        debug.Model
	activeView   view
	previousView view
}

func NewRouter(logPath, daemonURL string) Router {
	return Router{
		activeView:  sessionListView,
		sessionList: sessionlist.New(),
		sessionForm: sessionform.New(),
		todoList:    todolist.New(),
		todoForm:    todoform.New(),
		debug:       debug.New(logPath, daemonURL),
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
	case sessionlist.NewSessionMsg:
		return r.navigateTo(sessionFormView)
	case sessionlist.TodosMsg:
		return r.navigateTo(TodoListView)
	case sessionform.BackMsg:
		return r.navigateTo(sessionListView)
	case todolist.NewTodoMsg:
		return r.navigateTo(todoFormView)
	case todolist.BackMsg:
		return r.navigateTo(sessionListView)
	case todoform.BackMsg:
		return r.navigateTo(TodoListView)
	case todoform.DoneMsg:
		return r.navigateTo(TodoListView)
	case debug.BackMsg:
		r.activeView = r.previousView
		return r, nil
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

func (r Router) navigateTo(target view) (Router, tea.Cmd) {
	r.previousView = r.activeView
	r.activeView = target
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
