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
	workspacePickerView
	nameInputView
	debugView
	filePickerView
)

type App struct {
	activeView           view
	previousView         view
	sessionList          SessionListModel
	newSession           NewSessionModel
	nameInput            NameInputModel
	filePicker           FilePickerModel
	help                 help.Model
	pendingCreate        string
	pendingWorkspacePath string
	logPath              string
	width, height        int
}

var debugStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

func NewApp(logPath string) App {
	return App{
		activeView:  sessionListView,
		sessionList: NewSessionListModel(),
		newSession:  NewNewSessionModel(),
		help:        help.New(),
		logPath:     logPath,
	}
}

func (a App) Init() tea.Cmd {
	return fetchSessions()
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.sessionList.SetSize(msg.Width, msg.Height)
		a.newSession.SetSize(msg.Width, msg.Height)

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}
		if key.Matches(msg, debugKey) && a.activeView != debugView {
			a.previousView = a.activeView
			a.activeView = debugView
			return a, nil
		}
		if a.activeView == debugView && key.Matches(msg, backKey) {
			a.activeView = a.previousView
			return a, nil
		}

	case activateSessionMsg:
		return a, activateSession(msg.name)

	case sessionActivatedMsg:
		if a.pendingCreate != "" {
			return a, sendZellijPipe("create_session", msg.name, a.pendingWorkspacePath)
		}
		return a, sendZellijPipe("switch_session", msg.name, "")

	case switchToNewSessionMsg:
		a.activeView = workspacePickerView
		a.newSession = NewNewSessionModel()
		a.newSession.SetSize(a.width, a.height)
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
		a.pendingWorkspacePath = a.nameInput.workspace.Path
		return a, createSession(msg.name, msg.workspaceID)

	case sessionCreatedMsg:
		return a, activateSession(a.pendingCreate)

	case switchToFilePickerMsg:
		a.activeView = filePickerView
		a.filePicker = NewFilePickerModel()
		a.filePicker.SetSize(a.width, a.height)
		return a, a.filePicker.Init()

	case workspaceAddedMsg:
		a.activeView = workspacePickerView
		a.newSession = NewNewSessionModel()
		a.newSession.SetSize(a.width, a.height)
		return a, fetchWorkspaces()

	case pipeSentMsg:
		return a, tea.Quit

	case errMsg:
		switch a.activeView {
		case nameInputView:
			a.nameInput.err = msg.err.Error()
			return a, nil
		case sessionListView:
			return a, a.sessionList.list.NewStatusMessage(msg.err.Error())
		}
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
	case filePickerView:
		a.filePicker, cmd = a.filePicker.Update(msg)
	}
	return a, cmd
}

func (a App) View() string {
	switch a.activeView {
	case debugView:
		return a.debugViewContent()
	case workspacePickerView:
		return a.newSession.View()
	case nameInputView:
		return a.nameInput.View() + "\n\n" + a.help.View(nameInputKeyMap)
	case filePickerView:
		return a.filePicker.View()
	default:
		return a.sessionList.View()
	}
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

type errMsg struct{ err error }
