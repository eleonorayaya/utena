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

type App struct {
	sessions     SessionsProvider
	workspaces   WorkspacesProvider
	todos        TodosProvider
	sessionList  SessionListModel
	sessionForm  SessionFormModel
	todoList     TodoListModel
	todoForm     TodoFormModel
	debug        DebugModel
	help         help.Model
	activeView   view
	previousView view
	initialView  view
	width        int
	height       int
}

type AppOption func(*App)

func WithInitialView(v view) AppOption {
	return func(a *App) {
		a.initialView = v
		a.activeView = v
	}
}

func NewApp(logPath string, opts ...AppOption) App {
	a := App{
		activeView:  sessionListView,
		sessions:    NewSessionsProvider(),
		workspaces:  NewWorkspacesProvider(),
		todos:       NewTodosProvider(),
		sessionList: NewSessionListModel(),
		sessionForm: NewSessionFormModel(),
		todoList:    NewTodoListModel(),
		todoForm:    NewTodoFormModel(),
		debug:       NewDebugModel(logPath),
		help:        help.New(),
	}
	for _, opt := range opts {
		opt(&a)
	}
	return a
}

func (a App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		a.sessions.Init(),
		fetchWorkspaces(),
	}
	if a.initialView == TodoListView {
		cmds = append(cmds, fetchTodos())
	}
	return tea.Batch(cmds...)
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		return a.onWindowSizeMsg(wsm)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}
		if key.Matches(msg, appKeys.Debug) && a.activeView != debugView {
			a.previousView = a.activeView
			a.activeView = debugView
			return a, nil
		}

	case navigateMsg:
		return a.onNavigate(msg)
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	a.sessions, cmd = a.sessions.Update(msg)
	cmds = append(cmds, cmd)
	a.workspaces, cmd = a.workspaces.Update(msg)
	cmds = append(cmds, cmd)
	a.todos, cmd = a.todos.Update(msg)
	cmds = append(cmds, cmd)

	switch a.activeView {
	case sessionListView:
		a.sessionList, cmd = a.sessionList.Update(msg)
	case sessionFormView:
		a.sessionForm, cmd = a.sessionForm.Update(msg)
	case TodoListView:
		a.todoList, cmd = a.todoList.Update(msg)
	case todoFormView:
		a.todoForm, cmd = a.todoForm.Update(msg)
	case debugView:
		a.debug, cmd = a.debug.Update(msg)
	}
	cmds = append(cmds, cmd)

	return a, tea.Batch(cmds...)
}

func (a App) onNavigate(msg navigateMsg) (tea.Model, tea.Cmd) {
	if msg.target == backView {
		a.activeView = a.previousView
		return a, nil
	}
	a.previousView = a.activeView
	a.activeView = msg.target
	switch msg.target {
	case sessionListView:
		return a, tea.Batch(a.sessionList.Init(), a.sessions.Init())
	case sessionFormView:
		return a, a.sessionForm.Init()
	case TodoListView:
		return a, tea.Batch(a.todoList.Init(), fetchTodos())
	case todoFormView:
		return a, a.todoForm.Init()
	}
	return a, nil
}

func (a App) onWindowSizeMsg(wsm tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	a.width = wsm.Width
	a.height = wsm.Height
	msg := tea.WindowSizeMsg{Width: wsm.Width, Height: wsm.Height - 2}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	a.sessionList, cmd = a.sessionList.Update(msg)
	cmds = append(cmds, cmd)
	a.sessionForm, cmd = a.sessionForm.Update(msg)
	cmds = append(cmds, cmd)
	a.todoList, cmd = a.todoList.Update(msg)
	cmds = append(cmds, cmd)
	a.todoForm, cmd = a.todoForm.Update(msg)
	cmds = append(cmds, cmd)
	return a, tea.Batch(cmds...)
}

func (a App) View() string {
	var content string
	switch a.activeView {
	case sessionFormView:
		content = a.sessionForm.View()
	case TodoListView:
		content = a.todoList.View()
	case todoFormView:
		content = a.todoForm.View()
	case debugView:
		content = a.debug.View()
	default:
		content = a.sessionList.View()
	}

	keys := a.keys()
	helpView := a.help.View(keys)
	if helpView != "" {
		content += "\n" + helpView
	}
	return content
}

func (a App) keys() help.KeyMap {
	keymaps := []help.KeyMap{appKeys}

	switch a.activeView {
	case sessionListView:
		keymaps = append(keymaps, a.sessionList.Keys())
	case sessionFormView:
		keymaps = append(keymaps, a.sessionForm.Keys())
	case TodoListView:
		keymaps = append(keymaps, a.todoList.Keys())
	case todoFormView:
		keymaps = append(keymaps, a.todoForm.Keys())
	case debugView:
		keymaps = append(keymaps, a.debug.Keys())
	}

	return mergedKeyMap{keymaps: keymaps}
}

