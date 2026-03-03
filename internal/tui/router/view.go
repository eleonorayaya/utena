package router

type View int

const (
	SessionListView View = iota
	SessionFormView
	TodoListView
	TodoFormView
	DebugView
	StatusView
)
