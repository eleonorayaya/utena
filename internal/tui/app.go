package tui

import (
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
)

type view int

const (
	sessionListView view = iota
	workspacePickerView
	nameInputView
)

type App struct {
	activeView    view
	sessionList   SessionListModel
	newSession    NewSessionModel
	nameInput     NameInputModel
	help          help.Model
	pendingCreate string
}

func NewApp() App {
	return App{
		activeView:  sessionListView,
		sessionList: NewSessionListModel(),
		newSession:  NewNewSessionModel(),
		help:        help.New(),
	}
}

func (a App) Init() tea.Cmd {
	return fetchSessions()
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}

	case activateSessionMsg:
		return a, activateSession(msg.name)

	case sessionActivatedMsg:
		return a, tea.Quit

	case switchToNewSessionMsg:
		a.activeView = workspacePickerView
		a.newSession = NewNewSessionModel()
		return a, fetchWorkspaces()

	case switchToNameInputMsg:
		a.activeView = nameInputView
		a.nameInput = NewNameInputModel(msg.workspace)
		return a, a.nameInput.Init()

	case switchToSessionListMsg:
		a.activeView = sessionListView
		return a, fetchSessions()

	case createSessionMsg:
		a.pendingCreate = msg.name
		return a, createSession(msg.name, msg.workspaceID)

	case sessionCreatedMsg:
		return a, activateSession(a.pendingCreate)

	case errMsg:
		_ = msg.err
		return a, nil
	}

	var cmd tea.Cmd
	switch a.activeView {
	case sessionListView:
		a.sessionList, cmd = a.sessionList.Update(msg)
	case workspacePickerView:
		a.newSession, cmd = a.newSession.Update(msg)
	case nameInputView:
		a.nameInput, cmd = a.nameInput.Update(msg)
	}
	return a, cmd
}

func (a App) View() string {
	var content string
	var helpView string

	switch a.activeView {
	case workspacePickerView:
		content = a.newSession.View()
		helpView = a.help.View(workspacePickerKeyMap)
	case nameInputView:
		content = a.nameInput.View()
		helpView = a.help.View(nameInputKeyMap)
	default:
		content = a.sessionList.View()
		helpView = a.help.View(sessionListKeyMap)
	}

	return content + "\n\n" + helpView
}

type errMsg struct{ err error }
