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
	s, err := b.sessions.GetSession(ctx, sessionID)
	if errors.Is(err, session.ErrSessionNotFound) {
		return false, browser.ErrSessionNotFound
	}
	if err != nil {
		return false, err
	}
	return s.IsAttached, nil
}

func (b browserSessions) Activate(ctx context.Context, sessionID uint) error {
	_, err := b.sessions.ActivateSession(ctx, sessionID)
	return err
}
