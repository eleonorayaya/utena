package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/monitor"
	"github.com/stretchr/testify/require"
)

type lineCollector struct {
	lines chan string
}

func (l *lineCollector) Write(p []byte) (int, error) {
	select {
	case l.lines <- string(p):
	default:
	}
	return len(p), nil
}

func TestStreamSessionEventsWritesOneLinePerEvent(t *testing.T) {
	bus := eventbus.NewEventBus()
	module := monitor.NewMonitorModule(bus)
	require.NoError(t, module.OnAppStart(context.Background()))
	server := httptest.NewServer(module.Routes())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out := &lineCollector{lines: make(chan string, 1)}
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?session_id=5"
	go func() { _ = streamSessionEvents(ctx, url, out) }()

	publish := time.NewTicker(20 * time.Millisecond)
	defer publish.Stop()
	for {
		select {
		case line := <-out.lines:
			require.JSONEq(t, `{"type":"pull_request","session_id":5,"data":{"number":42,"state":"open"}}`, strings.TrimSpace(line))
			return
		case <-publish.C:
			require.NoError(t, bus.Publish(ctx, eventbus.Event{
				Type: eventbus.SessionNotification,
				Data: eventbus.SessionNotificationEvent{
					SessionID: 5,
					Type:      "pull_request",
					Data:      map[string]any{"number": 42, "state": "open"},
				},
			}))
		case <-ctx.Done():
			t.Fatal("timed out waiting for a session event line")
		}
	}
}
