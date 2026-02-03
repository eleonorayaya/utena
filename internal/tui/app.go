package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type keyMap struct {
	Quit key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Quit},
	}
}

type App struct {
	keys keyMap
	help help.Model
}

func (a App) Init() tea.Cmd {
	return fetchSessions()
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.QuitMsg:
		return a, tea.Quit

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, a.keys.Quit):
			return a, tea.Quit
		}

	case sessionsLoadedMsg:
		_ = msg.sessions

	case workspacesLoadedMsg:
		_ = msg.workspaces

	case sessionActivatedMsg:

	case sessionCreatedMsg:

	case errMsg:
		_ = msg.err
	}

	return a, nil
}

func (a App) View() string {
	s := "utena"
	s += "\n\n" + a.help.View(a.keys)
	return s
}

func NewApp() App {
	return App{
		keys: keyMap{
			Quit: key.NewBinding(
				key.WithKeys("q", "ctrl+c"),
				key.WithHelp("q", "quit"),
			),
		},
		help: help.New(),
	}
}

type errMsg struct{ err error }
