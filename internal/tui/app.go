package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type view int

const (
	sessionListView view = iota
	sessionFormView
	TodoListView
	todoFormView
	debugView
)

type App struct {
	sessions     SessionsProvider
	workspaces   WorkspacesProvider
	todos        TodosProvider
	sessionList  SessionListModel
	sessionForm  SessionFormModel
	todoList     TodoListModel
	todoForm     TodoFormModel
	help         help.Model
	activeView   view
	previousView view
	initialView  view
	logPath      string
	width        int
	height       int
}

var debugStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

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
		help:        help.New(),
		logPath:     logPath,
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
		if a.activeView == debugView && msg.String() == "esc" {
			a.activeView = a.previousView
			return a, nil
		}

	case openSessionFormMsg:
		a.activeView = sessionFormView
		return a, a.sessionForm.Init()

	case openTodosViewMsg:
		a.activeView = TodoListView
		return a, tea.Batch(a.todoList.Init(), fetchTodos())

	case openTodoFormMsg:
		a.activeView = todoFormView
		return a, a.todoForm.Init()

	case returnToSessionsMsg:
		a.activeView = sessionListView
		return a, tea.Batch(a.sessionList.Init(), a.sessions.Init())

	case sessionFormCancelledMsg:
		a.activeView = sessionListView
		return a, a.sessionList.Init()

	case todoFormCancelledMsg:
		a.activeView = TodoListView
		return a, a.todoList.Init()

	case pipeSentMsg:
		return a, tea.Quit
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
	}
	cmds = append(cmds, cmd)

	return a, tea.Batch(cmds...)
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
	if a.activeView == debugView {
		return a.debugViewContent()
	}

	var content string
	switch a.activeView {
	case sessionFormView:
		content = a.sessionForm.View()
	case TodoListView:
		content = a.todoList.View()
	case todoFormView:
		content = a.todoForm.View()
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
	}

	return mergedKeyMap{keymaps: keymaps}
}

func (a App) debugViewContent() string {
	var b strings.Builder
	b.WriteString(debugStyle.Render("Debug Info") + "\n\n")

	lines := []struct{ label, value string }{
		{"daemon", baseURL},
		{"log", a.logPath},
	}

	for _, l := range lines {
		v := l.value
		if v == "" {
			v = "(not set)"
		}
		b.WriteString(fmt.Sprintf("  %s: %s\n", debugStyle.Render(l.label), v))
	}

	b.WriteString("\n" + debugStyle.Render("esc: back"))
	return b.String()
}
