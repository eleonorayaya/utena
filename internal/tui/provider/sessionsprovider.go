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
	SessionName string
	Windows     []tmux.Window
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

func RepairSession(id uint) tea.Cmd {
	return func() tea.Msg { return repairSessionIntentMsg{id: id} }
}

func PollSession(id uint) tea.Cmd {
	return func() tea.Msg { return pollSessionIntentMsg{id: id} }
}

func DeleteSession(id uint) tea.Cmd {
	return func() tea.Msg { return deleteSessionIntentMsg{id: id} }
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
}

type repairSessionIntentMsg struct {
	id uint
}

type deleteSessionIntentMsg struct {
	id uint
}

type fetchWindowsIntentMsg struct {
	sessionName string
}

type windowsLoadedMsg struct {
	sessionName string
	windows     []tmux.Window
}

type setActiveWorkspaceMsg struct {
	workspaceID uint
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
	ID     uint
	Status session.SessionStatus
}

type sessionRepairedMsg struct {
	id uint
}

type sessionDeletedMsg struct {
	id uint
}

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
		return setActiveWorkspaceMsg{workspaceID: 0}
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
			return WindowsStateUpdatedMsg{SessionName: msg.sessionName, Windows: msg.windows}
		}

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
		return p, p.client.createSession(msg.name, msg.workspaceID, msg.branch, msg.baseBranch)

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
