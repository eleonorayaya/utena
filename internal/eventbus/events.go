package eventbus

const (
	TmuxSessionCreated       = "tmux.session_created"
	TmuxSessionClosed        = "tmux.session_closed"
	TmuxClientSessionChanged = "tmux.client_session_changed"
	TmuxClientAttached       = "tmux.client_attached"
	TmuxClientDetached       = "tmux.client_detached"

	SessionActivated = "session.activated"

	SessionNotification = "monitor.session_notification"
)

type TmuxHookEvent struct {
	TmuxSessionName string
}

type SessionActivatedEvent struct {
	SessionName string
}

type SessionNotificationEvent struct {
	SessionID uint
	Type      string
	Data      any
}
