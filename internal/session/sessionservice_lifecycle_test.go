package session

import (
	"context"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/stretchr/testify/require"
)

func recordLifecycle(service *SessionService, eventType string) func() []eventbus.SessionIDEvent {
	var got []eventbus.SessionIDEvent
	service.eventBus.Subscribe(eventType, func(_ context.Context, event eventbus.Event) error {
		if data, ok := event.Data.(eventbus.SessionIDEvent); ok {
			got = append(got, data)
		}
		return nil
	})
	return func() []eventbus.SessionIDEvent { return got }
}

func TestSessionService_ArchiveSession_PublishesSessionArchived(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)
	sess := &Session{Name: "session-1", Status: StatusActive, LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, swtStoreFor(service), sess, ws1ID)
	got := recordLifecycle(service, eventbus.SessionArchived)

	_, err := service.ArchiveSession(context.Background(), sess.ID)
	require.NoError(t, err)

	require.Equal(t, []eventbus.SessionIDEvent{{SessionID: sess.ID}}, got())
}

func TestSessionService_ArchiveSession_RefusedPublishesNothing(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)
	sess := &Session{Name: "session-1", Status: StatusArchived, LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, swtStoreFor(service), sess, ws1ID)
	got := recordLifecycle(service, eventbus.SessionArchived)

	_, err := service.ArchiveSession(context.Background(), sess.ID)
	require.Error(t, err)

	require.Empty(t, got())
}

func TestSessionService_ArchiveSession_PublishErrorIsNotReturned(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)
	sess := &Session{Name: "session-1", Status: StatusActive, LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, swtStoreFor(service), sess, ws1ID)
	service.eventBus.Subscribe(eventbus.SessionArchived, func(context.Context, eventbus.Event) error {
		return context.Canceled
	})

	archived, err := service.ArchiveSession(context.Background(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, StatusArchived, archived.Status)
}

func TestSessionService_DeleteSession_PublishesSessionDeleted(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)
	sess := &Session{Name: "session-1", Status: StatusArchived, LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, swtStoreFor(service), sess, ws1ID)
	got := recordLifecycle(service, eventbus.SessionDeleted)

	require.NoError(t, service.DeleteSession(context.Background(), sess.ID, false, false))

	require.Equal(t, []eventbus.SessionIDEvent{{SessionID: sess.ID}}, got())
}

func TestSessionService_DeleteSession_RefusedPublishesNothing(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)
	sess := &Session{Name: "session-1", Status: StatusActive, LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, swtStoreFor(service), sess, ws1ID)
	got := recordLifecycle(service, eventbus.SessionDeleted)

	require.Error(t, service.DeleteSession(context.Background(), sess.ID, false, false))

	require.Empty(t, got())
}

func TestSessionService_DeleteSession_PublishErrorIsNotReturned(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)
	sess := &Session{Name: "session-1", Status: StatusArchived, LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, swtStoreFor(service), sess, ws1ID)
	service.eventBus.Subscribe(eventbus.SessionDeleted, func(context.Context, eventbus.Event) error {
		return context.Canceled
	})

	require.NoError(t, service.DeleteSession(context.Background(), sess.ID, false, false))
}
