package tui

import (
	"fmt"
	"log"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/addworkspaceform"
	"github.com/eleonorayaya/utena/internal/tui/blockerform"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/sessionform"
	"github.com/eleonorayaya/utena/internal/tui/sessionlist"
	"github.com/eleonorayaya/utena/internal/tui/sessionprogress"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/eleonorayaya/utena/internal/tui/workspacecloneform"
	"github.com/eleonorayaya/utena/internal/tui/workspacelist"
)

const padX = 2
const padY = 1

type App struct {
	provider provider.Provider
	router   router.Router
	help     helpModel
	park     func() error
	width    int
	height   int
}

type Option func(*options)

type options struct {
	list []sessionlist.Option
	park func() error
}

type ParkMsg struct{}

func ParkFilter(_ tea.Model, msg tea.Msg) tea.Msg {
	if _, ok := msg.(tea.QuitMsg); ok {
		return ParkMsg{}
	}
	return msg
}

func WithPark(park func() error) Option {
	return func(o *options) { o.park = park }
}

func WithEscQuits() Option {
	return func(o *options) { o.list = append(o.list, sessionlist.WithEscQuits()) }
}

func NewApp(port string, initialView router.View, opts ...Option) App {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%s", port)
	views := map[router.View]router.ViewEntry{
		router.SessionListView:        &router.ViewAdapter[sessionlist.Model]{Model: sessionlist.New(o.list...)},
		router.SessionFormView:        &router.ViewAdapter[sessionform.Model]{Model: sessionform.New()},
		router.SessionProgressView:    &router.ViewAdapter[sessionprogress.Model]{Model: sessionprogress.New()},
		router.WorkspaceListView:      &router.ViewAdapter[workspacelist.Model]{Model: workspacelist.New()},
		router.WorkspaceCloneFormView: &router.ViewAdapter[workspacecloneform.Model]{Model: workspacecloneform.New()},
		router.AddWorkspaceFormView:   &router.ViewAdapter[addworkspaceform.Model]{Model: addworkspaceform.New()},
		router.BlockerFormView:        &router.ViewAdapter[blockerform.Model]{Model: blockerform.New()},
	}

	return App{
		provider: provider.NewRootProvider(baseURL),
		router:   router.New(views, initialView),
		help:     newHelpModel(),
		park:     o.park,
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
	case ParkMsg:
		return a.onPark()
	case tea.FocusMsg:
		if a.park != nil {
			return a, tea.Batch(provider.FetchSessions(), provider.RequestBrowserState())
		}
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if a.park != nil {
				return a, tea.Interrupt
			}
			return a, tea.Quit
		}
		if a.help.full {
			if key.Matches(msg, ui.MoreKey) || msg.Type == tea.KeyEsc {
				a.help = a.help.SetFull(false)
			}
			return a, nil
		}
		if key.Matches(msg, ui.MoreKey) && !a.router.CapturesText() {
			a.help = a.help.SetFull(true)
			return a, nil
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

func (a App) onPark() (tea.Model, tea.Cmd) {
	a.help = a.help.SetFull(false)
	var cmd tea.Cmd
	a.router, cmd = a.router.Reset(router.SessionListView)
	park := a.park
	return a, tea.Batch(cmd, func() tea.Msg {
		if park != nil {
			if err := park(); err != nil {
				log.Printf("[ERROR] park picker: %v", err)
			}
		}
		return nil
	})
}

func (a App) OnWindowSizeMsg(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	a.width = msg.Width
	a.height = msg.Height
	adjusted := tea.WindowSizeMsg{
		Width:  msg.Width - padX*2,
		Height: msg.Height - padY*2 - a.help.HeightCost(),
	}
	var cmd tea.Cmd
	a.router, cmd = a.router.Update(adjusted)
	return a, cmd
}

func (a App) View() string {
	width := max(a.width-padX*2, 0)
	pad := lipgloss.NewStyle().Padding(padY, padX)
	if a.help.full {
		return pad.Render(ui.FitHeight(a.help.FullView(a.keys(), width), max(a.height-padY*2, 1)))
	}
	content := a.router.View()
	showMore := !a.router.CapturesText()
	if footer := a.help.Footer(a.keys(), a.router.Status(), width, showMore); footer != "" {
		content += "\n\n" + footer
	}
	return pad.Render(content)
}

func (a App) keys() help.KeyMap {
	return mergedKeyMap{keymaps: []help.KeyMap{appKeys, a.router.Keys()}}
}
