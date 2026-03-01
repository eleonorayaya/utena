package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/eleonorayaya/utena/internal/zellij"
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

	// Verify hard-coded workspaces
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

	require.Equal(t, http.StatusCreated, w.Code)

	var createResp session.SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &createResp)
	require.NoError(t, err)
	require.Equal(t, "utena-test-session-1", createResp.ID)
	require.Equal(t, "test-session-1", createResp.Name)

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
		require.Equal(t, http.StatusCreated, w.Code)
	}

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

func TestDaemon_ZellijSessionUpdate(t *testing.T) {
	_, router := setupTestRouter(t)

	updateReq := &zellij.UpdateSessionsRequest{
		Sessions: []zellij.SessionUpdate{
			{
				Name:             "main-session",
				ConnectedClients: 1,
			},
			{
				Name:             "background-session",
				ConnectedClients: 0,
			},
		},
	}

	body, err := json.Marshal(updateReq)
	require.NoError(t, err)

	req := httptest.NewRequest("PUT", "/zellij/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response map[string]string
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "ok", response["status"])

	req = httptest.NewRequest("GET", "/sessions", nil)
	w = httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var sessionsResponse session.SessionListResponse
	err = json.Unmarshal(w.Body.Bytes(), &sessionsResponse)
	require.NoError(t, err)
	require.Len(t, sessionsResponse.Sessions, 2)

	mainSession := findSessionByID(sessionsResponse.Sessions, "main-session")
	require.NotNil(t, mainSession)
	require.True(t, mainSession.IsAttached)
	require.True(t, mainSession.IsActive)
	require.False(t, mainSession.IsDead)

	bgSession := findSessionByID(sessionsResponse.Sessions, "background-session")
	require.NotNil(t, bgSession)
	require.False(t, bgSession.IsAttached)
	require.True(t, bgSession.IsActive)
	require.False(t, bgSession.IsDead)
}

func findSessionByID(sessions []session.Session, id string) *session.Session {
	for _, s := range sessions {
		if s.ID == id {
			return &s
		}
	}
	return nil
}

func TestDaemon_ZellijSessionUpdate_MarkDeadSessions(t *testing.T) {
	_, router := setupTestRouter(t)

	req1 := httptest.NewRequest("POST", "/sessions", bytes.NewReader([]byte(`{"name":"old-session-1","workspace_id":"ws-1"}`)))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	require.Equal(t, http.StatusCreated, w1.Code)

	req2 := httptest.NewRequest("POST", "/sessions", bytes.NewReader([]byte(`{"name":"old-session-2","workspace_id":"ws-1"}`)))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusCreated, w2.Code)

	updateReq := &zellij.UpdateSessionsRequest{
		Sessions: []zellij.SessionUpdate{
			{
				Name:             "utena-old-session-1",
				ConnectedClients: 1,
			},
			{
				Name:             "new-session",
				ConnectedClients: 0,
			},
		},
	}

	body, err := json.Marshal(updateReq)
	require.NoError(t, err)

	req := httptest.NewRequest("PUT", "/zellij/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest("GET", "/sessions", nil)
	w = httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var sessionsResponse session.SessionListResponse
	err = json.Unmarshal(w.Body.Bytes(), &sessionsResponse)
	require.NoError(t, err)
	require.Len(t, sessionsResponse.Sessions, 3)

	oldSession1 := findSessionByID(sessionsResponse.Sessions, "utena-old-session-1")
	require.NotNil(t, oldSession1)
	require.True(t, oldSession1.IsAttached)
	require.False(t, oldSession1.IsDead)

	oldSession2 := findSessionByID(sessionsResponse.Sessions, "utena-old-session-2")
	require.NotNil(t, oldSession2)
	require.True(t, oldSession2.IsDead)

	newSession := findSessionByID(sessionsResponse.Sessions, "new-session")
	require.NotNil(t, newSession)
	require.False(t, newSession.IsAttached)
	require.False(t, newSession.IsDead)
}
