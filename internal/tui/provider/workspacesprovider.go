package provider

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type WorkspacesStateUpdatedMsg struct {
	Workspaces        []workspace.Workspace
	ActiveWorkspaceID uint
}

type BranchesLoadedMsg struct {
	Branches []string
}

type BranchRef = git.BranchRef

type BranchesFetchedMsg struct {
	Branches []BranchRef
	Err      error
}

type BranchExistsCheckedMsg struct {
	Name         string
	ExistsLocal  bool
	ExistsRemote bool
	Err          error
}

func FetchWorkspaces() tea.Cmd {
	return func() tea.Msg { return fetchWorkspacesIntentMsg{} }
}

func RequestWorkspacesState() tea.Cmd {
	return func() tea.Msg { return requestWorkspacesStateMsg{} }
}

func RequestBranches(workspaceID uint) tea.Cmd {
	return func() tea.Msg { return requestBranchesMsg{workspaceID: workspaceID} }
}

func FetchOriginBranches(workspaceID uint) tea.Cmd {
	return func() tea.Msg { return fetchOriginBranchesIntentMsg{workspaceID: workspaceID} }
}

func CheckBranchExists(workspaceID uint, name string) tea.Cmd {
	return func() tea.Msg { return checkBranchExistsIntentMsg{workspaceID: workspaceID, name: name} }
}

type PRsStateUpdatedMsg struct {
	PullRequests []git.PullRequest
	WorkspaceID  uint
}

func FetchPRs(workspaceID uint, state string) tea.Cmd {
	return func() tea.Msg { return fetchPRsIntentMsg{workspaceID: workspaceID, state: state} }
}

func DeleteWorkspace(id uint) tea.Cmd {
	return func() tea.Msg { return deleteWorkspaceIntentMsg{id: id} }
}

func MigrateWorkspaceToBare(id uint) tea.Cmd {
	return func() tea.Msg { return migrateWorkspaceToBareIntentMsg{id: id} }
}

func SetWorkspaceHidden(id uint, hidden bool) tea.Cmd {
	return func() tea.Msg { return setWorkspaceHiddenIntentMsg{id: id, hidden: hidden} }
}

func AddWorkspace(path string, asRoot bool) tea.Cmd {
	return func() tea.Msg { return addWorkspaceIntentMsg{path: path, asRoot: asRoot} }
}

type WorkspaceClonedMsg struct {
	Workspace workspace.Workspace
	Err       error
}

type WorkspaceRootsMsg struct {
	Roots []string
	Err   error
}

func CloneWorkspace(cloneURL, rootPath, dirName string) tea.Cmd {
	return func() tea.Msg {
		return cloneWorkspaceIntentMsg{cloneURL: cloneURL, rootPath: rootPath, dirName: dirName}
	}
}

func FetchWorkspaceRoots() tea.Cmd {
	return func() tea.Msg { return fetchWorkspaceRootsIntentMsg{} }
}

type fetchWorkspacesIntentMsg struct{}
type requestWorkspacesStateMsg struct{}

type requestBranchesMsg struct {
	workspaceID uint
}

type fetchOriginBranchesIntentMsg struct {
	workspaceID uint
}

type checkBranchExistsIntentMsg struct {
	workspaceID uint
	name        string
}

type branchesFetchedLoadedMsg struct {
	branches []BranchRef
	err      error
}

type branchExistsCheckedLoadedMsg struct {
	name         string
	existsLocal  bool
	existsRemote bool
	err          error
}

type deleteWorkspaceIntentMsg struct {
	id uint
}

type setWorkspaceHiddenIntentMsg struct {
	id     uint
	hidden bool
}

type addWorkspaceIntentMsg struct {
	path   string
	asRoot bool
}

type cloneWorkspaceIntentMsg struct {
	cloneURL string
	rootPath string
	dirName  string
}

type fetchWorkspaceRootsIntentMsg struct{}

type workspaceClonedMsg struct {
	workspace workspace.Workspace
	err       error
}

type workspaceRootsMsg struct {
	roots []string
	err   error
}

type fetchPRsIntentMsg struct {
	workspaceID uint
	state       string
}

type prsLoadedMsg struct {
	prs         []git.PullRequest
	workspaceID uint
}

type workspacesLoadedMsg struct {
	workspaces []workspace.Workspace
}

type branchesLoadedMsg struct {
	branches []string
}

type workspaceDeletedMsg struct{}
type workspaceHiddenToggledMsg struct{}
type workspaceAddedMsg struct{}
type workspaceMigratedToBareMsg struct{}

type migrateWorkspaceToBareIntentMsg struct {
	id uint
}

type workspacesProvider struct {
	client            *client
	workspaces        []workspace.Workspace
	branches          []string
	activeWorkspaceID uint
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

	case fetchOriginBranchesIntentMsg:
		return p, p.client.fetchOriginBranches(msg.workspaceID)

	case branchesFetchedLoadedMsg:
		branches := msg.branches
		err := msg.err
		return p, func() tea.Msg { return BranchesFetchedMsg{Branches: branches, Err: err} }

	case checkBranchExistsIntentMsg:
		return p, p.client.checkBranchExists(msg.workspaceID, msg.name)

	case branchExistsCheckedLoadedMsg:
		name := msg.name
		existsLocal := msg.existsLocal
		existsRemote := msg.existsRemote
		err := msg.err
		return p, func() tea.Msg {
			return BranchExistsCheckedMsg{Name: name, ExistsLocal: existsLocal, ExistsRemote: existsRemote, Err: err}
		}

	case fetchPRsIntentMsg:
		return p, p.client.fetchPRs(msg.workspaceID, msg.state)

	case prsLoadedMsg:
		prs := msg.prs
		wsID := msg.workspaceID
		return p, func() tea.Msg { return PRsStateUpdatedMsg{PullRequests: prs, WorkspaceID: wsID} }

	case setActiveWorkspaceMsg:
		p.activeWorkspaceID = msg.workspaceID
		return p, p.emitState()

	case deleteWorkspaceIntentMsg:
		return p, p.client.deleteWorkspace(msg.id)

	case workspaceDeletedMsg:
		return p, p.client.fetchWorkspaces()

	case setWorkspaceHiddenIntentMsg:
		return p, p.client.setWorkspaceHidden(msg.id, msg.hidden)

	case workspaceHiddenToggledMsg:
		return p, p.client.fetchWorkspaces()

	case addWorkspaceIntentMsg:
		return p, p.client.addWorkspace(msg.path, msg.asRoot)

	case workspaceAddedMsg:
		return p, p.client.fetchWorkspaces()

	case migrateWorkspaceToBareIntentMsg:
		return p, p.client.migrateWorkspaceToBare(msg.id)

	case workspaceMigratedToBareMsg:
		return p, p.client.fetchWorkspaces()

	case cloneWorkspaceIntentMsg:
		return p, p.client.cloneWorkspace(msg.cloneURL, msg.rootPath, msg.dirName)

	case workspaceClonedMsg:
		ws := msg.workspace
		err := msg.err
		var refetch tea.Cmd
		if err == nil {
			refetch = p.client.fetchWorkspaces()
		}
		emit := func() tea.Msg { return WorkspaceClonedMsg{Workspace: ws, Err: err} }
		if refetch == nil {
			return p, emit
		}
		return p, tea.Batch(emit, refetch)

	case fetchWorkspaceRootsIntentMsg:
		return p, p.client.fetchWorkspaceRoots()

	case workspaceRootsMsg:
		roots := msg.roots
		err := msg.err
		return p, func() tea.Msg { return WorkspaceRootsMsg{Roots: roots, Err: err} }
	}

	return p, nil
}
