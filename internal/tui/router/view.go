package router

type View int

const (
	SessionListView View = iota
	SessionFormView
	SessionProgressView
	WorkspaceListView
	WorkspaceCloneFormView
	AddWorkspaceFormView
	BlockerFormView
)
