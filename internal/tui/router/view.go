package router

type View int

const (
	SessionListView View = iota
	SessionFormView
	SessionProgressView
	TodoListView
	TodoFormView
	DebugView
	StatusView
	WorkspaceListView
	WorkspaceDetailView
	WorkspaceCloneFormView
	PRListView
	SessionDetailView
)
