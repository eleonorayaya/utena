package mux

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nextActivationBody(t *testing.T, svc *Service, event eventbus.SessionActivatedEvent) (int, string) {
	t.Helper()
	routes := NewRouter(NewController(svc)).Routes()
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		routes.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/activations/next", nil))
	}()
	require.Eventually(t, func() bool {
		svc.activationsMu.Lock()
		defer svc.activationsMu.Unlock()
		return len(svc.activationWaiters) == 1
	}, time.Second, time.Millisecond)
	require.NoError(t, svc.eventBus.Publish(context.Background(), eventbus.Event{Type: eventbus.SessionActivated, Data: event}))
	<-done
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

func TestController_NextActivationLocalBodyHasNoHost(t *testing.T) {
	svc, _ := newTestService(t)
	code, body := nextActivationBody(t, svc, eventbus.SessionActivatedEvent{SessionName: "work"})
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, `{"session_name":"work"}`, body)
}

func TestController_NextActivationCarriesHost(t *testing.T) {
	svc, _ := newTestService(t)
	code, body := nextActivationBody(t, svc, eventbus.SessionActivatedEvent{SessionName: "work", Host: "ws-1"})
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, `{"session_name":"work","host":"ws-1"}`, body)
}

func TestController_HookPassesHost(t *testing.T) {
	svc, _ := newTestService(t)
	var got []eventbus.MuxHookEvent
	svc.eventBus.Subscribe(eventbus.MuxClientAttached, func(_ context.Context, e eventbus.Event) error {
		got = append(got, e.Data.(eventbus.MuxHookEvent))
		return nil
	})
	routes := NewRouter(NewController(svc)).Routes()
	for _, body := range []string{`{"session_name":"a"}`, `{"session_name":"b","host":"ws-1"}`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/hooks/client-attached", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		routes.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	}
	assert.Equal(t, []eventbus.MuxHookEvent{{SessionName: "a"}, {SessionName: "b", Host: "ws-1"}}, got)
}
