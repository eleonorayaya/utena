package eventbus

const (
	MuxSessionCreated       = "mux.session_created"
	MuxSessionClosed        = "mux.session_closed"
	MuxClientSessionChanged = "mux.client_session_changed"
	MuxClientAttached       = "mux.client_attached"
	MuxClientDetached       = "mux.client_detached"

	SessionActivated = "session.activated"
	SessionAttached  = "session.attached"
	SessionArchived  = "session.archived"
	SessionDeleted   = "session.deleted"

	SessionNotification = "session.notification"
)

type MuxHookEvent struct {
	SessionName string
}

type SessionActivatedEvent struct {
	SessionName string
}

type SessionIDEvent struct {
	SessionID uint
}

type SessionNotificationEvent struct {
	SessionID uint   `json:"session_id"`
	Type      string `json:"type"`
	Data      any    `json:"data,omitempty"`
}
