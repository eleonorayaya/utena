package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type WorkspacesProvider struct {
	workspaces        []workspace.Workspace
	activeWorkspaceID string
}

func NewWorkspacesProvider() WorkspacesProvider {
	return WorkspacesProvider{}
}

func (p WorkspacesProvider) emitState() tea.Cmd {
	workspaces := p.workspaces
	activeWorkspaceID := p.activeWorkspaceID
	return func() tea.Msg {
		return workspacesStateUpdatedMsg{
			workspaces:        workspaces,
			activeWorkspaceID: activeWorkspaceID,
		}
	}
}

func (p WorkspacesProvider) Update(msg tea.Msg) (WorkspacesProvider, tea.Cmd) {
	switch msg := msg.(type) {
	case workspacesLoadedMsg:
		p.workspaces = msg.workspaces
		return p, p.emitState()

	case requestWorkspacesStateMsg:
		return p, p.emitState()

	case setActiveWorkspaceMsg:
		p.activeWorkspaceID = msg.workspaceID
		return p, p.emitState()

	case addWorkspaceIntentMsg:
		return p, addWorkspace(msg.path, msg.asRoot)

	case workspaceAddedMsg:
		return p, fetchWorkspaces()
	}

	return p, nil
}
