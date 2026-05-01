package provider

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
)

type SessionsStateUpdatedMsg struct {
	Sessions []session.Session
}

func FetchSessions() tea.Cmd {
	return func() tea.Msg { return fetchSessionsIntentMsg{} }
}

func RequestSessionsState() tea.Cmd {
	return func() tea.Msg { return requestSessionsStateMsg{} }
}

func ActivateSession(id uint) tea.Cmd {
	return func() tea.Msg { return activateSessionMsg{id: id} }
}

func CreateSession(name string, workspaceID uint, branch, baseBranch, workspacePath string) tea.Cmd {
	return func() tea.Msg {
		return createSessionIntentMsg{
			name:          name,
			workspaceID:   workspaceID,
			branch:        branch,
			baseBranch:    baseBranch,
			workspacePath: workspacePath,
		}
	}
}

func CreateSessionFromTodo(name string, workspaceID uint, todoID uint) tea.Cmd {
	return func() tea.Msg {
		return createSessionIntentMsg{
			name:        name,
			workspaceID: workspaceID,
			todoID:      &todoID,
		}
	}
}

func RepairSession(id uint) tea.Cmd {
	return func() tea.Msg { return repairSessionIntentMsg{id: id} }
}

func PollSession(id uint) tea.Cmd {
	return func() tea.Msg { return pollSessionIntentMsg{id: id} }
}

func DeleteSession(id uint) tea.Cmd {
	return func() tea.Msg { return deleteSessionIntentMsg{id: id} }
}

func ArchiveSession(id uint) tea.Cmd {
	return func() tea.Msg { return archiveSessionIntentMsg{id: id} }
}

type fetchSessionsIntentMsg struct{}
type requestSessionsStateMsg struct{}

type activateSessionMsg struct {
	id uint
}

type createSessionIntentMsg struct {
	name          string
	workspaceID   uint
	branch        string
	baseBranch    string
	workspacePath string
	todoID        *uint
}

type repairSessionIntentMsg struct {
	id uint
}

type deleteSessionIntentMsg struct {
	id uint
}

type archiveSessionIntentMsg struct {
	id uint
}

type setActiveWorkspaceMsg struct {
	workspaceID uint
}

type sessionsLoadedMsg struct {
	sessions []session.Session
}

type sessionActivatedMsg struct {
	tmuxSessionName string
}

type SessionCreatedMsg struct {
	ID     uint
	Status session.SessionStatus
}

type sessionRepairedMsg struct {
	id uint
}

type sessionDeletedMsg struct {
	id uint
}

type sessionArchivedMsg struct{ id uint }

type sessionPolledMsg struct {
	session session.Session
}

type pollSessionIntentMsg struct {
	id uint
}

type SessionPolledMsg struct {
	Session session.Session
}

type SessionSwitchedMsg struct{}

type sessionsProvider struct {
	client   *client
	sessions []session.Session
}

func newSessionsProvider(c *client) sessionsProvider {
	return sessionsProvider{client: c}
}

func (p sessionsProvider) emitState() tea.Cmd {
	sessions := p.sessions
	return func() tea.Msg {
		return SessionsStateUpdatedMsg{
			Sessions: sessions,
		}
	}
}

func (p sessionsProvider) deriveActiveWorkspace() tea.Cmd {
	for _, s := range p.sessions {
		if s.IsAttached {
			wid := s.WorkspaceID
			return func() tea.Msg {
				return setActiveWorkspaceMsg{workspaceID: wid}
			}
		}
	}
	return func() tea.Msg {
		return setActiveWorkspaceMsg{workspaceID: 0}
	}
}

func (p sessionsProvider) Update(msg tea.Msg) (sessionsProvider, tea.Cmd) {
	switch msg := msg.(type) {
	case sessionsLoadedMsg:
		p.sessions = msg.sessions
		return p, tea.Batch(p.emitState(), p.deriveActiveWorkspace())

	case fetchSessionsIntentMsg:
		return p, p.client.fetchSessions()

	case requestSessionsStateMsg:
		return p, p.emitState()

	case activateSessionMsg:
		id := msg.id
		return p, func() tea.Msg {
			return p.client.activateSession(id)()
		}

	case repairSessionIntentMsg:
		id := msg.id
		return p, p.client.repairSession(id)

	case sessionRepairedMsg:
		return p, p.client.fetchSessions()

	case createSessionIntentMsg:
		return p, p.client.createSession(msg.name, msg.workspaceID, msg.branch, msg.baseBranch, msg.todoID)

	case SessionCreatedMsg:
		return p, p.client.fetchSessions()

	case sessionActivatedMsg:
		return p, p.client.switchTmuxSession(msg.tmuxSessionName)

	case deleteSessionIntentMsg:
		return p, p.client.deleteSession(msg.id)

	case sessionDeletedMsg:
		return p, p.client.fetchSessions()

	case archiveSessionIntentMsg:
		return p, p.client.archiveSession(msg.id)

	case sessionArchivedMsg:
		return p, p.client.fetchSessions()

	case pollSessionIntentMsg:
		return p, p.client.getSession(msg.id)

	case sessionPolledMsg:
		return p, func() tea.Msg {
			return SessionPolledMsg{Session: msg.session}
		}
	}

	return p, nil
}
