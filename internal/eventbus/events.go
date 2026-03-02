package eventbus

const (
	SessionCreateRequested = "session.create_requested"
	SessionActivated       = "session.activated"
	SessionRevived         = "session.revived"
)

type SessionCreateRequestedEvent struct {
	SessionName   string
	WorkspacePath string
}

type SessionActivatedEvent struct {
	SessionName string
}

type SessionRevivedEvent struct {
	SessionName   string
	WorkspacePath string
}
