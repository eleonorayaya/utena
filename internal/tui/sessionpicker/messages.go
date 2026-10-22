package sessionpicker

import "github.com/eleonorayaya/utena/internal/session"

type SelectedMsg struct {
	Session session.Session
}

type ClosedMsg struct{}
