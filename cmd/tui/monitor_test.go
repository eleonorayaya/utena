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

type stubSnapshots struct{}

func (stubSnapshots) SessionSnapshot(_ context.Context, sessionID uint) []eventbus.SessionNotificationEvent {
	return []eventbus.SessionNotificationEvent{{
		SessionID: sessionID,
		Type:      "pull_request",
		Data:      map[string]any{"number": 42, "state": "open"},
	}}
}

type lineCollector struct {
	lines chan string
}

func (l *lineCollector) Write(p []byte) (int, error) {
	l.lines <- string(p)
	return len(p), nil
}

func TestStreamSessionEventsWritesOneLinePerEvent(t *testing.T) {
	module := monitor.NewMonitorModule(eventbus.NewEventBus(), stubSnapshots{})
	require.NoError(t, module.OnAppStart(context.Background()))
	server := httptest.NewServer(module.Routes())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out := &lineCollector{lines: make(chan string)}
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?session_id=5"
	go func() { _ = streamSessionEvents(ctx, url, out) }()

	select {
	case line := <-out.lines:
		require.JSONEq(t, `{"type":"pull_request","session_id":5,"data":{"number":42,"state":"open"}}`, strings.TrimSpace(line))
	case <-ctx.Done():
		t.Fatal("timed out waiting for a session event line")
	}
}
