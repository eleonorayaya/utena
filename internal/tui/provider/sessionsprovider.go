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

type SessionWorkspaceSpec struct {
	WorkspaceID uint
	Branch      string
	BaseBranch  string
}

func CreateSession(name string, specs []SessionWorkspaceSpec) tea.Cmd {
	return func() tea.Msg {
		return createSessionIntentMsg{
			name:  name,
			specs: specs,
		}
	}
}

func CreateSessionFromTodo(name string, workspaceID uint, todoID uint) tea.Cmd {
	specs := []SessionWorkspaceSpec{{WorkspaceID: workspaceID, BaseBranch: "main"}}
	return func() tea.Msg {
		return createSessionIntentMsg{
			name:   name,
			specs:  specs,
			todoID: &todoID,
		}
	}
}

func RepairSession(id uint) tea.Cmd {
	return func() tea.Msg { return repairSessionIntentMsg{id: id} }
}

func PollSession(id uint) tea.Cmd {
	return func() tea.Msg { return pollSessionIntentMsg{id: id} }
}

func DeleteSession(id uint, force bool) tea.Cmd {
	return func() tea.Msg { return deleteSessionIntentMsg{id: id, force: force} }
}

func ArchiveSession(id uint) tea.Cmd {
	return func() tea.Msg { return archiveSessionIntentMsg{id: id} }
}

func AddWorkspaceToSession(sessionID uint, spec SessionWorkspaceSpec) tea.Cmd {
	return func() tea.Msg { return addWorkspaceToSessionIntentMsg{sessionID: sessionID, spec: spec} }
}

type fetchSessionsIntentMsg struct{}
type requestSessionsStateMsg struct{}

type activateSessionMsg struct {
	id uint
}

type createSessionIntentMsg struct {
	name   string
	specs  []SessionWorkspaceSpec
	todoID *uint
}

type repairSessionIntentMsg struct {
	id uint
}

type deleteSessionIntentMsg struct {
	id    uint
	force bool
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

type addWorkspaceToSessionIntentMsg struct {
	sessionID uint
	spec      SessionWorkspaceSpec
}

type workspaceAddedToSessionMsg struct {
	sessionID uint
}

type WorkspaceAddedToSessionMsg struct {
	SessionID uint
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
		if !s.IsAttached {
			continue
		}
		var wid uint
		if !s.IsMulti() && len(s.Worktrees) > 0 && s.Worktrees[0].Workspace != nil {
			wid = s.Worktrees[0].Workspace.ID
		}
		return func() tea.Msg {
			return setActiveWorkspaceMsg{workspaceID: wid}
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
		return p, p.client.createSession(msg.name, msg.specs, msg.todoID)

	case SessionCreatedMsg:
		return p, p.client.fetchSessions()

	case sessionActivatedMsg:
		return p, p.client.switchTmuxSession(msg.tmuxSessionName)

	case deleteSessionIntentMsg:
		return p, p.client.deleteSession(msg.id, msg.force)

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

	case addWorkspaceToSessionIntentMsg:
		return p, p.client.addWorkspaceToSession(msg.sessionID, msg.spec)

	case workspaceAddedToSessionMsg:
		id := msg.sessionID
		return p, tea.Batch(
			p.client.fetchSessions(),
			func() tea.Msg { return WorkspaceAddedToSessionMsg{SessionID: id} },
		)
	}

	return p, nil
}
