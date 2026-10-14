package mux

import "context"

type Driver interface {
	NewSession(name, startDir string, env map[string]string) error
	NewWindow(session, startDir, command string, env map[string]string) error
	KillSession(name string) error
	HasSession(name string) bool
	ListSessions() ([]string, error)
	ListWindows(session string) ([]Window, error)
	Subscribe(ctx context.Context, onEvent func(Event)) error
}

type EventType string

const (
	EventResync         EventType = "resync"
	EventSessionCreated EventType = "session_created"
	EventSessionClosed  EventType = "session_closed"
	EventWindowsChanged EventType = "windows_changed"
)

type Event struct {
	Type    EventType
	Session string
}
