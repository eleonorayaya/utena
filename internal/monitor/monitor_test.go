package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/stretchr/testify/require"
)

func setup(t *testing.T) (*eventbus.InMemoryEventBus, *MonitorService, string) {
	t.Helper()

	bus := eventbus.NewEventBus()
	module := NewMonitorModule(bus)
	require.NoError(t, module.OnAppStart(context.Background()))

	server := httptest.NewServer(module.Routes())
	t.Cleanup(server.Close)

	return bus, module.Service, "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
}

func waitForClient(t *testing.T, service *MonitorService, sessionID uint) {
	t.Helper()

	require.Eventually(t, func() bool {
		service.mu.Lock()
		defer service.mu.Unlock()
		return len(service.clients[sessionID]) > 0
	}, 5*time.Second, 10*time.Millisecond)
}

func readMessage(t *testing.T, ctx context.Context, sock *websocket.Conn) eventbus.SessionNotificationEvent {
	t.Helper()

	kind, data, err := sock.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, websocket.MessageText, kind)

	var got eventbus.SessionNotificationEvent
	require.NoError(t, json.Unmarshal(data, &got))
	return got
}

func TestWatchBroadcastsSessionNotifications(t *testing.T) {
	bus, service, wsURL := setup(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sock, _, err := websocket.Dial(ctx, wsURL+"?session_id=7", nil)
	require.NoError(t, err)
	defer func() { _ = sock.CloseNow() }()
	waitForClient(t, service, 7)

	require.NoError(t, bus.Publish(ctx, eventbus.Event{
		Type: eventbus.SessionNotification,
		Data: eventbus.SessionNotificationEvent{
			SessionID: 7,
			Type:      "pull_request",
			Data:      map[string]any{"number": 42},
		},
	}))

	got := readMessage(t, ctx, sock)
	require.Equal(t, "pull_request", got.Type)
	require.Equal(t, uint(7), got.SessionID)
	require.Equal(t, map[string]any{"number": float64(42)}, got.Data)
}

func TestWatchIgnoresOtherSessions(t *testing.T) {
	bus, service, wsURL := setup(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sock, _, err := websocket.Dial(ctx, wsURL+"?session_id=1", nil)
	require.NoError(t, err)
	defer func() { _ = sock.CloseNow() }()
	waitForClient(t, service, 1)

	require.NoError(t, bus.Publish(ctx, eventbus.Event{
		Type: eventbus.SessionNotification,
		Data: eventbus.SessionNotificationEvent{SessionID: 2, Type: "pull_request"},
	}))

	readCtx, readCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer readCancel()
	_, _, err = sock.Read(readCtx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestWatchRequiresSessionID(t *testing.T) {
	_, _, wsURL := setup(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
