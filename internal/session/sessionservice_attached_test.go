package session

import (
	"context"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/stretchr/testify/require"
)

func recordSessionAttached(service *SessionService) func() []eventbus.SessionIDEvent {
	var got []eventbus.SessionIDEvent
	service.eventBus.Subscribe(eventbus.SessionAttached, func(_ context.Context, event eventbus.Event) error {
		if data, ok := event.Data.(eventbus.SessionIDEvent); ok {
			got = append(got, data)
		}
		return nil
	})
	return func() []eventbus.SessionIDEvent { return got }
}

func addAttachableSession(t *testing.T, service *SessionService, store *SessionStore, wsID uint, name string) *Session {
	t.Helper()
	sess := &Session{Name: name, Status: StatusActive, LastUsedAt: time.Now().Add(-time.Hour)}
	addTestSession(t, store, swtStoreFor(service), sess, wsID)
	muxRecord, err := service.muxService.RegisterPending(SanitizeMuxName(name), "/tmp/utena", nil)
	require.NoError(t, err)
	sess.MuxSessionID = &muxRecord.ID
	require.NoError(t, store.Update(sess))
	return sess
}

func TestSessionService_ClientSessionChanged_PublishesSessionAttached(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)
	sess := addAttachableSession(t, service, sessionStore, ws1ID, "session-1")
	got := recordSessionAttached(service)

	err := service.handleMuxClientSessionChanged(context.Background(), eventbus.Event{
		Type: eventbus.MuxClientSessionChanged,
		Data: eventbus.MuxHookEvent{SessionName: SanitizeMuxName(sess.Name)},
	})
	require.NoError(t, err)

	require.Equal(t, []eventbus.SessionIDEvent{{SessionID: sess.ID}}, got())
}

func TestSessionService_ClientAttached_PublishesSessionAttached(t *testing.T) {
	service, sessionStore, _, _, ws1ID, _ := setupSessionService(t)
	sess := addAttachableSession(t, service, sessionStore, ws1ID, "session-1")
	got := recordSessionAttached(service)

	err := service.handleMuxClientAttached(context.Background(), eventbus.Event{
		Type: eventbus.MuxClientAttached,
		Data: eventbus.MuxHookEvent{SessionName: SanitizeMuxName(sess.Name)},
	})
	require.NoError(t, err)

	require.Equal(t, []eventbus.SessionIDEvent{{SessionID: sess.ID}}, got())
}

func TestSessionService_ClientHooks_UnknownSessionPublishesNothing(t *testing.T) {
	service, _, _, _, _, _ := setupSessionService(t)
	got := recordSessionAttached(service)
	ctx := context.Background()
	event := eventbus.Event{Data: eventbus.MuxHookEvent{SessionName: "missing"}}

	require.NoError(t, service.handleMuxClientSessionChanged(ctx, event))
	require.NoError(t, service.handleMuxClientAttached(ctx, event))

	require.Empty(t, got())
}

func TestSessionService_ActivateSession_PublishesSessionAttached(t *testing.T) {
	service, sessionStore, _, muxDriver, ws1ID, _ := setupSessionService(t)
	sess := addAttachableSession(t, service, sessionStore, ws1ID, "session-1")
	require.NoError(t, muxDriver.NewSession(SanitizeMuxName(sess.Name), "", nil))
	got := recordSessionAttached(service)

	_, err := service.ActivateSession(context.Background(), sess.ID)
	require.NoError(t, err)

	require.Equal(t, []eventbus.SessionIDEvent{{SessionID: sess.ID}}, got())
}

func TestSessionService_ActivateSession_AlreadyAttachedPublishesNothing(t *testing.T) {
	service, sessionStore, _, muxDriver, ws1ID, _ := setupSessionService(t)
	sess := addAttachableSession(t, service, sessionStore, ws1ID, "session-1")
	require.NoError(t, muxDriver.NewSession(SanitizeMuxName(sess.Name), "", nil))
	sess.IsAttached = true
	require.NoError(t, sessionStore.Update(sess))
	got := recordSessionAttached(service)

	_, err := service.ActivateSession(context.Background(), sess.ID)
	require.NoError(t, err)

	require.Empty(t, got())
}
