package session

import (
	"context"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type SessionService struct {
	store          *SessionStore
	workspaceStore *workspace.WorkspaceStore
	eventBus       eventbus.EventBus
}

func NewSessionService(store *SessionStore, workspaceStore *workspace.WorkspaceStore, bus eventbus.EventBus) *SessionService {
	return &SessionService{
		store:          store,
		workspaceStore: workspaceStore,
		eventBus:       bus,
	}
}

func (s *SessionService) OnAppStart(ctx context.Context) error {
	return nil
}

func (s *SessionService) OnAppEnd(ctx context.Context) error {

	return nil
}

func (s *SessionService) ListSessions(ctx context.Context) ([]Session, error) {
	return s.store.List(), nil
}

func (s *SessionService) ListSessionsByWorkspace(ctx context.Context, workspaceID string) ([]Session, error) {

	_, err := s.workspaceStore.GetByID(workspaceID)
	if err != nil {
		return nil, err
	}

	return s.store.ListByWorkspace(workspaceID), nil
}

func (s *SessionService) GetSession(ctx context.Context, id string) (*Session, error) {
	return s.store.GetByID(id)
}

func (s *SessionService) CreateSession(ctx context.Context, session *Session) error {

	if session.WorkspaceID != "" {
		_, err := s.workspaceStore.GetByID(session.WorkspaceID)
		if err != nil {
			return err
		}
	}

	if session.LastUsedAt.IsZero() {
		session.LastUsedAt = time.Now()
	}

	if err := s.store.Add(session); err != nil {
		return err
	}

	return nil
}

func (s *SessionService) CreateSessionAndNotify(ctx context.Context, session *Session) error {
	if err := s.CreateSession(ctx, session); err != nil {
		return err
	}

	event := eventbus.Event{
		Type: eventbus.SessionCreateRequested,
		Data: eventbus.SessionCreateRequestedEvent{
			SessionName:   session.ID,
			WorkspacePath: "",
		},
	}
	s.eventBus.Publish(ctx, event)

	return nil
}

func (s *SessionService) UpdateSession(ctx context.Context, session *Session) error {

	if session.WorkspaceID != "" {
		_, err := s.workspaceStore.GetByID(session.WorkspaceID)
		if err != nil {
			return err
		}
	}

	return s.store.Update(session)
}

func (s *SessionService) ActivateSession(ctx context.Context, name string) (*Session, error) {
	session, err := s.store.GetByID(name)
	if err != nil {
		return nil, err
	}

	session.LastUsedAt = time.Now()
	session.IsActive = true

	if err := s.store.Update(session); err != nil {
		return nil, err
	}

	s.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.SessionActivated,
		Data: eventbus.SessionActivatedEvent{
			SessionName: name,
		},
	})

	return session, nil
}

func (s *SessionService) DeleteSession(ctx context.Context, id string) error {
	return s.store.Delete(id)
}
