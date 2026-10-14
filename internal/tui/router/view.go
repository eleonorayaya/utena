package router

type View int

const (
	SessionListView View = iota
	SessionFormView
	SessionProgressView
	WorkspaceListView
	WorkspaceCloneFormView
	SessionDetailView
	AddWorkspaceFormView
	BlockerFormView
)
