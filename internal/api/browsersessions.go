package api

import (
	"context"
	"errors"

	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/session"
)

type browserSessions struct {
	sessions *session.SessionService
}

var _ browser.Sessions = browserSessions{}

func (b browserSessions) IsAttached(ctx context.Context, sessionID uint) (bool, error) {
	attached, err := b.sessions.IsAttached(sessionID)
	if errors.Is(err, session.ErrSessionNotFound) {
		return false, browser.ErrSessionNotFound
	}
	return attached, err
}

func (b browserSessions) Activate(ctx context.Context, sessionID uint) error {
	_, err := b.sessions.ActivateSession(ctx, sessionID)
	return err
}

func (b browserSessions) Attached(ctx context.Context) (uint, error) {
	sessions, err := b.sessions.ListSessions(ctx)
	if err != nil {
		return 0, err
	}
	for _, s := range sessions {
		if s.IsAttached {
			return s.ID, nil
		}
	}
	return 0, nil
}
