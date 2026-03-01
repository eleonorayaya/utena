package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/provider"
)

type App struct {
	provider provider.Provider
	router   Router
	help     help.Model
	width    int
	height   int
}

type AppOption func(*App)

func WithInitialView(v view) AppOption {
	return func(a *App) {
		a.router.activeView = v
	}
}

func NewApp(logPath, port, pipeName string, opts ...AppOption) App {
	baseURL := fmt.Sprintf("http://localhost:%s", port)
	a := App{
		provider: provider.NewRootProvider(baseURL, pipeName),
		router:   NewRouter(logPath, baseURL),
		help:     help.New(),
	}
	for _, opt := range opts {
		opt(&a)
	}
	return a
}

func (a App) Init() tea.Cmd {
	_, cmd := a.router.Init()
	return cmd
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return a.OnWindowSizeMsg(msg)
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	a.provider, cmd = a.provider.Update(msg)
	cmds = append(cmds, cmd)

	a.router, cmd = a.router.Update(msg)
	cmds = append(cmds, cmd)

	return a, tea.Batch(cmds...)
}

func (a App) OnWindowSizeMsg(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	a.width = msg.Width
	a.height = msg.Height
	adjusted := tea.WindowSizeMsg{Width: msg.Width, Height: msg.Height - 2}
	var cmd tea.Cmd
	a.router, cmd = a.router.Update(adjusted)
	return a, cmd
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
