package provider

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tmux"
)

type SessionsStateUpdatedMsg struct {
	Sessions       []session.Session
	ClaudeSessions map[string][]claude.ClaudeSession
}

type WindowsStateUpdatedMsg struct {
	Windows []tmux.Window
}

func FetchSessions() tea.Cmd {
	return func() tea.Msg { return fetchSessionsIntentMsg{} }
}

func FetchWindows(sessionName string) tea.Cmd {
	return func() tea.Msg { return fetchWindowsIntentMsg{sessionName: sessionName} }
}

func RequestSessionsState() tea.Cmd {
	return func() tea.Msg { return requestSessionsStateMsg{} }
}

func ActivateSession(name string) tea.Cmd {
	return func() tea.Msg { return activateSessionMsg{name: name} }
}

func CreateSession(name, workspaceID, branch, workspacePath string, branchCreated bool) tea.Cmd {
	return func() tea.Msg {
		return createSessionIntentMsg{
			name:          name,
			workspaceID:   workspaceID,
			branch:        branch,
			workspacePath: workspacePath,
			branchCreated: branchCreated,
		}
	}
}

func RepairSession(id string) tea.Cmd {
	return func() tea.Msg { return repairSessionIntentMsg{id: id} }
}

func PollSession(id string) tea.Cmd {
	return func() tea.Msg { return pollSessionIntentMsg{id: id} }
}

func DeleteSession(id string) tea.Cmd {
	return func() tea.Msg { return deleteSessionIntentMsg{id: id} }
}

type fetchSessionsIntentMsg struct{}
type requestSessionsStateMsg struct{}

type activateSessionMsg struct {
	name string
}

type createSessionIntentMsg struct {
	name          string
	workspaceID   string
	branch        string
	workspacePath string
	branchCreated bool
}

type repairSessionIntentMsg struct {
	id string
}

type deleteSessionIntentMsg struct {
	id string
}

type fetchWindowsIntentMsg struct {
	sessionName string
}

type windowsLoadedMsg struct {
	windows []tmux.Window
}

type setActiveWorkspaceMsg struct {
	workspaceID string
}

type sessionsLoadedMsg struct {
	sessions []session.Session
}

type claudeSessionsLoadedMsg struct {
	claudeSessions []claude.ClaudeSession
}

type sessionActivatedMsg struct {
	tmuxSessionName string
}

type SessionCreatedMsg struct {
	ID     string
	Status session.SessionStatus
}

type sessionRepairedMsg struct {
	id string
}

type sessionDeletedMsg struct {
	id string
}

type sessionPolledMsg struct {
	session session.Session
}

type pollSessionIntentMsg struct {
	id string
}

type SessionPolledMsg struct {
	Session session.Session
}

type sessionsProvider struct {
	client         *client
	sessions       []session.Session
	claudeSessions map[string][]claude.ClaudeSession
}

func newSessionsProvider(c *client) sessionsProvider {
	return sessionsProvider{client: c}
}

func (p sessionsProvider) emitState() tea.Cmd {
	sessions := p.sessions
	claudeSessions := p.claudeSessions
	return func() tea.Msg {
		return SessionsStateUpdatedMsg{
			Sessions:       sessions,
			ClaudeSessions: claudeSessions,
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
		return setActiveWorkspaceMsg{workspaceID: ""}
	}
}

func (p sessionsProvider) Update(msg tea.Msg) (sessionsProvider, tea.Cmd) {
	switch msg := msg.(type) {
	case sessionsLoadedMsg:
		p.sessions = msg.sessions
		return p, tea.Batch(p.emitState(), p.deriveActiveWorkspace())

	case claudeSessionsLoadedMsg:
		p.claudeSessions = make(map[string][]claude.ClaudeSession)
		for _, cs := range msg.claudeSessions {
			p.claudeSessions[cs.SessionID] = append(p.claudeSessions[cs.SessionID], cs)
		}
		return p, p.emitState()

	case fetchSessionsIntentMsg:
		return p, tea.Batch(p.client.fetchSessions(), p.client.fetchClaudeSessions())

	case fetchWindowsIntentMsg:
		return p, p.client.fetchWindows(msg.sessionName)

	case windowsLoadedMsg:
		return p, func() tea.Msg {
			return WindowsStateUpdatedMsg{Windows: msg.windows}
		}

	case requestSessionsStateMsg:
		return p, p.emitState()

	case activateSessionMsg:
		name := msg.name
		return p, func() tea.Msg {
			return p.client.activateSession(name)()
		}

	case repairSessionIntentMsg:
		id := msg.id
		return p, p.client.repairSession(id)

	case sessionRepairedMsg:
		return p, p.client.fetchSessions()

	case createSessionIntentMsg:
		name := msg.name
		workspaceID := msg.workspaceID
		branch := msg.branch
		branchCreated := msg.branchCreated
		return p, p.client.createSession(name, workspaceID, branch, branchCreated)

	case SessionCreatedMsg:
		return p, p.client.fetchSessions()

	case sessionActivatedMsg:
		return p, p.client.switchTmuxSession(msg.tmuxSessionName)

	case deleteSessionIntentMsg:
		return p, p.client.deleteSession(msg.id)

	case sessionDeletedMsg:
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
