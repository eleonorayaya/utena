package provider

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type WorkspacesStateUpdatedMsg struct {
	Workspaces        []workspace.Workspace
	ActiveWorkspaceID string
}

type BranchesLoadedMsg struct {
	Branches []string
}

func FetchWorkspaces() tea.Cmd {
	return func() tea.Msg { return fetchWorkspacesIntentMsg{} }
}

func RequestWorkspacesState() tea.Cmd {
	return func() tea.Msg { return requestWorkspacesStateMsg{} }
}

func RequestBranches(workspaceID string) tea.Cmd {
	return func() tea.Msg { return requestBranchesMsg{workspaceID: workspaceID} }
}

func AddWorkspace(path string, asRoot bool) tea.Cmd {
	return func() tea.Msg { return addWorkspaceIntentMsg{path: path, asRoot: asRoot} }
}

type fetchWorkspacesIntentMsg struct{}
type requestWorkspacesStateMsg struct{}

type requestBranchesMsg struct {
	workspaceID string
}

type addWorkspaceIntentMsg struct {
	path   string
	asRoot bool
}

type workspacesLoadedMsg struct {
	workspaces []workspace.Workspace
}

type branchesLoadedMsg struct {
	branches []string
}

type workspaceAddedMsg struct{}

type workspacesProvider struct {
	client            *client
	workspaces        []workspace.Workspace
	branches          []string
	activeWorkspaceID string
}

func newWorkspacesProvider(c *client) workspacesProvider {
	return workspacesProvider{client: c}
}

func (p workspacesProvider) emitState() tea.Cmd {
	workspaces := p.workspaces
	activeWorkspaceID := p.activeWorkspaceID
	return func() tea.Msg {
		return WorkspacesStateUpdatedMsg{
			Workspaces:        workspaces,
			ActiveWorkspaceID: activeWorkspaceID,
		}
	}
}

func (p workspacesProvider) Update(msg tea.Msg) (workspacesProvider, tea.Cmd) {
	switch msg := msg.(type) {
	case workspacesLoadedMsg:
		p.workspaces = msg.workspaces
		return p, p.emitState()

	case fetchWorkspacesIntentMsg:
		return p, p.client.fetchWorkspaces()

	case requestWorkspacesStateMsg:
		return p, p.emitState()

	case branchesLoadedMsg:
		p.branches = msg.branches
		branches := p.branches
		return p, func() tea.Msg { return BranchesLoadedMsg{Branches: branches} }

	case requestBranchesMsg:
		return p, p.client.fetchBranches(msg.workspaceID)

	case setActiveWorkspaceMsg:
		p.activeWorkspaceID = msg.workspaceID
		return p, p.emitState()

	case addWorkspaceIntentMsg:
		return p, p.client.addWorkspace(msg.path, msg.asRoot)

	case workspaceAddedMsg:
		return p, p.client.fetchWorkspaces()
	}

	return p, nil
}
