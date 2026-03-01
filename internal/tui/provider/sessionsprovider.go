package provider

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
)

type SessionsStateUpdatedMsg struct {
	Sessions       []session.Session
	ClaudeSessions map[string][]claude.ClaudeSession
}

func FetchSessions() tea.Cmd {
	return func() tea.Msg { return fetchSessionsIntentMsg{} }
}

func RequestSessionsState() tea.Cmd {
	return func() tea.Msg { return requestSessionsStateMsg{} }
}

func ActivateSession(name string) tea.Cmd {
	return func() tea.Msg { return activateSessionMsg{name: name} }
}

func CreateSession(name, workspaceID, branch, workspacePath string) tea.Cmd {
	return func() tea.Msg {
		return createSessionIntentMsg{
			name:          name,
			workspaceID:   workspaceID,
			branch:        branch,
			workspacePath: workspacePath,
		}
	}
}

func ReviveSession(name string) tea.Cmd {
	return func() tea.Msg { return reviveSessionIntentMsg{name: name} }
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
}

type reviveSessionIntentMsg struct {
	name string
}

type deleteSessionIntentMsg struct {
	id string
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
	name          string
	pipeCommand   string
	workspacePath string
}

type sessionCreatedMsg struct {
	worktreePath string
}

type sessionRevivedMsg struct {
	name          string
	workspacePath string
	worktreePath  string
}

type sessionDeletedMsg struct {
	id string
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

	case requestSessionsStateMsg:
		return p, p.emitState()

	case activateSessionMsg:
		name := msg.name
		return p, func() tea.Msg {
			result := p.client.activateSession(name)()
			if activated, ok := result.(sessionActivatedMsg); ok {
				return sessionActivatedMsg{
					name:        activated.name,
					pipeCommand: "switch_session",
				}
			}
			return result
		}

	case reviveSessionIntentMsg:
		name := msg.name
		return p, func() tea.Msg {
			reviveResult := p.client.reviveSession(name)()
			if err, ok := reviveResult.(ErrMsg); ok {
				return err
			}
			revived, ok := reviveResult.(sessionRevivedMsg)
			if !ok {
				return ErrMsg{Err: fmt.Errorf("unexpected result from reviveSession")}
			}
			wp := revived.workspacePath
			if revived.worktreePath != "" {
				wp = revived.worktreePath
			}

			activateResult := p.client.activateSession(name)()
			if err, ok := activateResult.(ErrMsg); ok {
				return err
			}
			return sessionActivatedMsg{
				name:          name,
				pipeCommand:   "create_session",
				workspacePath: wp,
			}
		}

	case createSessionIntentMsg:
		name := msg.name
		workspaceID := msg.workspaceID
		branch := msg.branch
		workspacePath := msg.workspacePath
		return p, func() tea.Msg {
			createResult := p.client.createSession(name, workspaceID, branch)()
			if err, ok := createResult.(ErrMsg); ok {
				return err
			}
			created, ok := createResult.(sessionCreatedMsg)
			if !ok {
				return ErrMsg{Err: fmt.Errorf("unexpected result from createSession")}
			}
			wp := workspacePath
			if created.worktreePath != "" {
				wp = created.worktreePath
			}

			activateResult := p.client.activateSession(name)()
			if err, ok := activateResult.(ErrMsg); ok {
				return err
			}
			return sessionActivatedMsg{
				name:          name,
				pipeCommand:   "create_session",
				workspacePath: wp,
			}
		}

	case sessionActivatedMsg:
		return p, p.client.sendZellijPipe(msg.pipeCommand, msg.name, msg.workspacePath)

	case deleteSessionIntentMsg:
		return p, p.client.deleteSession(msg.id)

	case sessionDeletedMsg:
		return p, p.client.fetchSessions()
	}

	return p, nil
}
