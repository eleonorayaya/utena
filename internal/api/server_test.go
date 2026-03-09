package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func setupTestRouter(t *testing.T) (*App, chi.Router) {
	t.Helper()

	cfg := Config{ConfigDir: "/config"}
	app := NewTestApp(cfg)

	app.Workspace.Store.Add(&workspace.Workspace{ID: "ws-1", Name: "utena", Path: "/tmp/utena"})
	app.Workspace.Store.Add(&workspace.Workspace{ID: "ws-2", Name: "other", Path: "/tmp/other"})

	err := app.OnStart(context.Background())
	require.NoError(t, err)

	server := BuildServer(app, cfg)

	return app, server.Handler.(*chi.Mux)
}

func waitForSessionStatus(t *testing.T, router chi.Router, id string, status session.SessionStatus, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest("GET", "/sessions/"+id, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			var resp session.SessionResponse
			if json.Unmarshal(w.Body.Bytes(), &resp) == nil && resp.Status == status {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("session %q did not reach status %q within %v", id, status, timeout)
}

func TestDaemon_ListWorkspaces(t *testing.T) {
	_, router := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/workspaces", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response workspace.WorkspaceListResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Len(t, response.Workspaces, 2)

	ids := make(map[string]bool)
	for _, ws := range response.Workspaces {
		ids[ws.ID] = true
	}
	require.True(t, ids["ws-1"])
	require.True(t, ids["ws-2"])
}

func TestDaemon_GetWorkspaceByID(t *testing.T) {
	_, router := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/workspaces/ws-1", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response workspace.WorkspaceResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "ws-1", response.ID)
	require.Equal(t, "utena", response.Name)
}

func TestDaemon_CreateAndGetSession(t *testing.T) {
	_, router := setupTestRouter(t)

	body := []byte(`{"name":"test-session-1","workspace_id":"ws-1"}`)

	req := httptest.NewRequest("POST", "/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)

	var createResp session.SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &createResp)
	require.NoError(t, err)
	require.Equal(t, "utena-test-session-1", createResp.ID)
	require.Equal(t, "test-session-1", createResp.Name)
	require.Equal(t, session.StatusCreating, createResp.Status)

	waitForSessionStatus(t, router, "utena-test-session-1", session.StatusReady, 2*time.Second)

	req = httptest.NewRequest("GET", "/sessions/utena-test-session-1", nil)
	w = httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response session.SessionResponse
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "utena-test-session-1", response.ID)
	require.Equal(t, "ws-1", response.WorkspaceID)
}

func TestDaemon_ListSessions(t *testing.T) {
	_, router := setupTestRouter(t)

	bodies := []string{
		`{"name":"session-1","workspace_id":"ws-1"}`,
		`{"name":"session-2","workspace_id":"ws-2"}`,
	}

	for _, b := range bodies {
		req := httptest.NewRequest("POST", "/sessions", bytes.NewReader([]byte(b)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusAccepted, w.Code)
	}

	waitForSessionStatus(t, router, "utena-session-1", session.StatusReady, 2*time.Second)
	waitForSessionStatus(t, router, "other-session-2", session.StatusReady, 2*time.Second)

	req := httptest.NewRequest("GET", "/sessions", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response session.SessionListResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Len(t, response.Sessions, 2)

	ids := make(map[string]bool)
	for _, s := range response.Sessions {
		ids[s.ID] = true
	}
	require.True(t, ids["utena-session-1"])
	require.True(t, ids["other-session-2"])
}

func TestDaemon_TmuxHookSessionCreated(t *testing.T) {
	_, router := setupTestRouter(t)

	body := []byte(`{"name":"test-session","workspace_id":"ws-1"}`)
	req := httptest.NewRequest("POST", "/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)

	waitForSessionStatus(t, router, "utena-test-session", session.StatusReady, 2*time.Second)

	hookBody := []byte(`{"session_name":"utena-test-session"}`)
	req = httptest.NewRequest("PUT", "/tmux/hooks/session-created", bytes.NewReader(hookBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "ok", response["status"])

	req = httptest.NewRequest("GET", "/sessions", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var sessionsResponse session.SessionListResponse
	err = json.Unmarshal(w.Body.Bytes(), &sessionsResponse)
	require.NoError(t, err)
	require.Len(t, sessionsResponse.Sessions, 1)

	sess := findSessionByID(sessionsResponse.Sessions, "utena-test-session")
	require.NotNil(t, sess)
	require.Equal(t, session.StatusReady, sess.Status)
}

func findSessionByID(sessions []session.Session, id string) *session.Session {
	for _, s := range sessions {
		if s.ID == id {
			return &s
		}
	}
	return nil
}

func TestDaemon_TmuxHookSessionClosed(t *testing.T) {
	_, router := setupTestRouter(t)

	body := []byte(`{"name":"test-session","workspace_id":"ws-1"}`)
	req := httptest.NewRequest("POST", "/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)

	waitForSessionStatus(t, router, "utena-test-session", session.StatusReady, 2*time.Second)

	hookBody := []byte(`{"session_name":"utena-test-session"}`)
	req = httptest.NewRequest("PUT", "/tmux/hooks/session-closed", bytes.NewReader(hookBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest("GET", "/sessions", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var sessionsResponse session.SessionListResponse
	err := json.Unmarshal(w.Body.Bytes(), &sessionsResponse)
	require.NoError(t, err)
	require.Len(t, sessionsResponse.Sessions, 1)

	sess := findSessionByID(sessionsResponse.Sessions, "utena-test-session")
	require.NotNil(t, sess)
	require.Equal(t, session.StatusBroken, sess.Status)
	require.False(t, sess.IsAttached)
}
