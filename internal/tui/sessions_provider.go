package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
)

type SessionsProvider struct {
	sessions       []session.Session
	claudeSessions map[string][]claude.ClaudeSession
}

func NewSessionsProvider() SessionsProvider {
	return SessionsProvider{}
}

func (p SessionsProvider) Init() tea.Cmd {
	return tea.Batch(fetchSessions(), fetchClaudeSessions())
}

func (p SessionsProvider) emitState() tea.Cmd {
	sessions := p.sessions
	claudeSessions := p.claudeSessions
	return func() tea.Msg {
		return sessionsStateUpdatedMsg{
			sessions:       sessions,
			claudeSessions: claudeSessions,
		}
	}
}

func (p SessionsProvider) deriveActiveWorkspace() tea.Cmd {
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

func (p SessionsProvider) Update(msg tea.Msg) (SessionsProvider, tea.Cmd) {
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

	case requestSessionsStateMsg:
		return p, p.emitState()

	case activateSessionMsg:
		name := msg.name
		return p, func() tea.Msg {
			result := activateSession(name)()
			if activated, ok := result.(sessionActivatedMsg); ok {
				return sessionActivatedMsg{
					name:        activated.name,
					pipeCommand: "switch_session",
				}
			}
			return result
		}

	case createSessionIntentMsg:
		name := msg.name
		workspaceID := msg.workspaceID
		branch := msg.branch
		workspacePath := msg.workspacePath
		return p, func() tea.Msg {
			createResult := createSession(name, workspaceID, branch)()
			if err, ok := createResult.(errMsg); ok {
				return err
			}
			created, ok := createResult.(sessionCreatedMsg)
			if !ok {
				return errMsg{err: fmt.Errorf("unexpected result from createSession")}
			}
			wp := workspacePath
			if created.worktreePath != "" {
				wp = created.worktreePath
			}

			activateResult := activateSession(name)()
			if err, ok := activateResult.(errMsg); ok {
				return err
			}
			return sessionActivatedMsg{
				name:          name,
				pipeCommand:   "create_session",
				workspacePath: wp,
			}
		}

	case sessionActivatedMsg:
		return p, sendZellijPipe(msg.pipeCommand, msg.name, msg.workspacePath)

	case deleteSessionIntentMsg:
		return p, deleteSession(msg.id)

	case sessionDeletedMsg:
		return p, fetchSessions()
	}

	return p, nil
}
