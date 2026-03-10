package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/debug"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/sessionform"
	"github.com/eleonorayaya/utena/internal/tui/sessionlist"
	"github.com/eleonorayaya/utena/internal/tui/sessionprogress"
	"github.com/eleonorayaya/utena/internal/tui/statusview"
	"github.com/eleonorayaya/utena/internal/tui/todoform"
	"github.com/eleonorayaya/utena/internal/tui/todolist"
)

type App struct {
	provider provider.Provider
	router   router.Router
	help     helpModel
	width    int
	height   int
}

func NewApp(logPath, port string, initialView router.View) App {
	baseURL := fmt.Sprintf("http://localhost:%s", port)
	views := map[router.View]router.ViewEntry{
		router.SessionListView:     &router.ViewAdapter[sessionlist.Model]{Model: sessionlist.New()},
		router.SessionFormView:     &router.ViewAdapter[sessionform.Model]{Model: sessionform.New()},
		router.SessionProgressView: &router.ViewAdapter[sessionprogress.Model]{Model: sessionprogress.New()},
		router.TodoListView:        &router.ViewAdapter[todolist.Model]{Model: todolist.New()},
		router.TodoFormView:        &router.ViewAdapter[todoform.Model]{Model: todoform.New()},
		router.DebugView:           &router.ViewAdapter[debug.Model]{Model: debug.New(logPath, baseURL)},
		router.StatusView:          &router.ViewAdapter[statusview.Model]{Model: statusview.New()},
	}

	return App{
		provider: provider.NewRootProvider(baseURL),
		router:   router.New(views, initialView),
		help:     newHelpModel(initialView != router.StatusView),
	}
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

	a.help = a.help.Update(msg)

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
	adjusted := tea.WindowSizeMsg{Width: msg.Width, Height: msg.Height - a.help.HeightCost()}
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
