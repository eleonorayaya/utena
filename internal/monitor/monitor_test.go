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

type fakeSnapshots struct {
	events []eventbus.SessionNotificationEvent
}

func (f *fakeSnapshots) SessionSnapshot(_ context.Context, sessionID uint) []eventbus.SessionNotificationEvent {
	return f.events
}

func setup(t *testing.T, snapshots SnapshotProvider) (*eventbus.InMemoryEventBus, string) {
	t.Helper()

	bus := eventbus.NewEventBus()
	module := NewMonitorModule(bus, snapshots)
	require.NoError(t, module.OnAppStart(context.Background()))

	server := httptest.NewServer(module.Routes())
	t.Cleanup(server.Close)

	return bus, "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
}

func readMessage(t *testing.T, ctx context.Context, sock *websocket.Conn) notification {
	t.Helper()

	kind, data, err := sock.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, websocket.MessageText, kind)

	var got notification
	require.NoError(t, json.Unmarshal(data, &got))
	return got
}

func TestWatchBroadcastsSessionNotifications(t *testing.T) {
	bus, wsURL := setup(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sock, _, err := websocket.Dial(ctx, wsURL+"?session_id=7", nil)
	require.NoError(t, err)
	defer func() { _ = sock.CloseNow() }()

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

func TestWatchSendsSnapshotOnConnect(t *testing.T) {
	snapshots := &fakeSnapshots{events: []eventbus.SessionNotificationEvent{{
		SessionID: 3,
		Type:      "pull_request",
		Data:      map[string]any{"number": 1},
	}}}
	_, wsURL := setup(t, snapshots)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sock, _, err := websocket.Dial(ctx, wsURL+"?session_id=3", nil)
	require.NoError(t, err)
	defer func() { _ = sock.CloseNow() }()

	got := readMessage(t, ctx, sock)
	require.Equal(t, "pull_request", got.Type)
	require.Equal(t, uint(3), got.SessionID)
}

func TestWatchIgnoresOtherSessions(t *testing.T) {
	bus, wsURL := setup(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sock, _, err := websocket.Dial(ctx, wsURL+"?session_id=1", nil)
	require.NoError(t, err)
	defer func() { _ = sock.CloseNow() }()

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
	_, wsURL := setup(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
