package eventbus

const (
	SessionCreateRequested = "session.create_requested"
	SessionActivated       = "session.activated"
)

type SessionCreateRequestedEvent struct {
	SessionName   string
	WorkspacePath string
}

type SessionActivatedEvent struct {
	SessionName string
}
