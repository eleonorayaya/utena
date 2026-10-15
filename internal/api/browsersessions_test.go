package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserRoutesMounted(t *testing.T) {
	_, router, _, _, _ := setupTestRouter(t)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/browser/status", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"connected":false}`, w.Body.String())

	body, _ := json.Marshal(map[string]any{"session_id": 1, "url": "https://x.example.com"})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/browser/open", bytes.NewReader(body)))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestBrowserSessions_UnknownSessionIsBrowserNotFound(t *testing.T) {
	app, _, _, _, _ := setupTestRouter(t)
	_, err := browserSessions{sessions: app.Session.Service}.IsAttached(context.Background(), 9999)
	assert.ErrorIs(t, err, browser.ErrSessionNotFound)
}

func TestBrowserSessions_AttachedReturnsAttachedSession(t *testing.T) {
	app, _, _, _, _ := setupTestRouter(t)
	b := browserSessions{sessions: app.Session.Service}

	id, err := b.Attached(context.Background())
	require.NoError(t, err)
	assert.Zero(t, id)

	require.NoError(t, app.Session.Store.Add(&session.Session{Name: "detached", Status: session.StatusActive}))
	attached := &session.Session{Name: "attached", Status: session.StatusActive, IsAttached: true}
	require.NoError(t, app.Session.Store.Add(attached))

	id, err = b.Attached(context.Background())
	require.NoError(t, err)
	assert.Equal(t, attached.ID, id)
}
