package tui

import (
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
)

type App struct {
	sessions    SessionsProvider
	workspaces  WorkspacesProvider
	todos       TodosProvider
	router      Router
	help        help.Model
	initialView view
	width       int
	height      int
}

type AppOption func(*App)

func WithInitialView(v view) AppOption {
	return func(a *App) {
		a.initialView = v
		a.router.activeView = v
	}
}

func NewApp(logPath string, opts ...AppOption) App {
	a := App{
		sessions:   NewSessionsProvider(),
		workspaces: NewWorkspacesProvider(),
		todos:      NewTodosProvider(),
		router:     NewRouter(logPath),
		help:       help.New(),
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
		a.width = wsm.Width
		a.height = wsm.Height
		adjusted := tea.WindowSizeMsg{Width: wsm.Width, Height: wsm.Height - 2}
		var cmd tea.Cmd
		a.router, cmd = a.router.Update(adjusted)
		return a, cmd
	}

	if msg, ok := msg.(tea.KeyMsg); ok && msg.String() == "ctrl+c" {
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

	a.router, cmd = a.router.Update(msg)
	cmds = append(cmds, cmd)

	return a, tea.Batch(cmds...)
}

func (a App) View() string {
	content := a.router.View()
	keys := a.keys()
	helpView := a.help.View(keys)
	if helpView != "" {
		content += "\n" + helpView
	}
	return content
}

func (a App) keys() help.KeyMap {
	return mergedKeyMap{keymaps: []help.KeyMap{appKeys, a.router.Keys()}}
}
