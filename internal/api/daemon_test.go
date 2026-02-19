package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/eleonorayaya/utena/internal/zellij"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupTestRouter(t *testing.T) chi.Router {
	t.Helper()

	ctx := context.Background()

	bus := eventbus.NewEventBus()

	workspaceModule := workspace.NewWorkspaceModule()
	sessionModule := session.NewSessionModule(workspaceModule, bus, afero.NewMemMapFs(), "/config")
	zellijModule := zellij.NewZellijModule(sessionModule, bus)

	workspaceModule.Store.Add(&workspace.Workspace{ID: "ws-1", Name: "utena", Path: "/tmp/utena"})
	workspaceModule.Store.Add(&workspace.Workspace{ID: "ws-2", Name: "other", Path: "/tmp/other"})

	err := workspaceModule.OnAppStart(ctx)
	require.NoError(t, err)

	err = zellijModule.OnAppStart(ctx)
	require.NoError(t, err)

	// Setup router
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.URLFormat)
	r.Use(render.SetContentType(render.ContentTypeJSON))

	// Mount module routers
	r.Mount("/workspaces", workspaceModule.Routes())
	r.Mount("/sessions", sessionModule.Routes())
	r.Mount("/zellij", zellijModule.Routes())

	return r
}

func TestDaemon_ListWorkspaces(t *testing.T) {
	router := setupTestRouter(t)

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
	router := setupTestRouter(t)

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
	router := setupTestRouter(t)

	// Create session
	sess := &session.Session{
		ID:          "test-session-1",
		WorkspaceID: "ws-1",
		IsAttached:  true,
		IsActive:    true,
		LastUsedAt:  time.Now(),
	}
	body, err := json.Marshal(sess)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)

	// Get session
	req = httptest.NewRequest("GET", "/sessions/test-session-1", nil)
	w = httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response session.SessionResponse
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "test-session-1", response.ID)
	require.Equal(t, "ws-1", response.WorkspaceID)
	require.True(t, response.IsAttached)
}

func TestDaemon_ListSessions(t *testing.T) {
	router := setupTestRouter(t)

	// Create multiple sessions
	sessions := []*session.Session{
		{
			ID:          "session-1",
			WorkspaceID: "ws-1",
			LastUsedAt:  time.Now().Add(-1 * time.Hour),
		},
		{
			ID:          "session-2",
			WorkspaceID: "ws-2",
			LastUsedAt:  time.Now(),
		},
	}

	for _, sess := range sessions {
		body, err := json.Marshal(sess)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/sessions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code)
	}

	// List sessions
	req := httptest.NewRequest("GET", "/sessions", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response session.SessionListResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Len(t, response.Sessions, 2)

	// Verify MRU sorting (most recent first)
	require.Equal(t, "session-2", response.Sessions[0].ID)
	require.Equal(t, "session-1", response.Sessions[1].ID)
}

func TestDaemon_ZellijSessionUpdate(t *testing.T) {
	router := setupTestRouter(t)

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
	router := setupTestRouter(t)

	sess1 := &session.Session{
		ID:          "old-session-1",
		WorkspaceID: "ws-1",
		IsActive:    true,
		IsDead:      false,
		LastUsedAt:  time.Now(),
	}
	sess2 := &session.Session{
		ID:          "old-session-2",
		WorkspaceID: "ws-1",
		IsActive:    true,
		IsDead:      false,
		LastUsedAt:  time.Now(),
	}

	body1, err := json.Marshal(sess1)
	require.NoError(t, err)
	req1 := httptest.NewRequest("POST", "/sessions", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	require.Equal(t, http.StatusCreated, w1.Code)

	body2, err := json.Marshal(sess2)
	require.NoError(t, err)
	req2 := httptest.NewRequest("POST", "/sessions", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusCreated, w2.Code)

	updateReq := &zellij.UpdateSessionsRequest{
		Sessions: []zellij.SessionUpdate{
			{
				Name:             "old-session-1",
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

	oldSession1 := findSessionByID(sessionsResponse.Sessions, "old-session-1")
	require.NotNil(t, oldSession1)
	require.True(t, oldSession1.IsAttached)
	require.False(t, oldSession1.IsDead)

	oldSession2 := findSessionByID(sessionsResponse.Sessions, "old-session-2")
	require.NotNil(t, oldSession2)
	require.True(t, oldSession2.IsDead)

	newSession := findSessionByID(sessionsResponse.Sessions, "new-session")
	require.NotNil(t, newSession)
	require.False(t, newSession.IsAttached)
	require.False(t, newSession.IsDead)
}
