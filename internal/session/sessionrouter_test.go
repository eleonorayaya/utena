package session

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupSessionRouter(t *testing.T) (*SessionRouter, *SessionStore, *workspace.WorkspaceStore) {
	t.Helper()

	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(afero.NewMemMapFs(), "/config")
	workspaceStore := workspace.NewWorkspaceStore(afero.NewMemMapFs(), "/config")

	workspaceStore.Add(&workspace.Workspace{ID: "ws-1", Name: "utena", Path: "/tmp/utena"})
	workspaceStore.Add(&workspace.Workspace{ID: "ws-2", Name: "other", Path: "/tmp/other"})

	gitService := git.NewGitService()
	service := NewSessionService(sessionStore, workspaceStore, gitService, bus)
	controller := NewSessionController(service)
	router := NewSessionRouter(controller)

	return router, sessionStore, workspaceStore
}

func TestSessionRouter_ListSessions(t *testing.T) {
	router, sessionStore, _ := setupSessionRouter(t)

	// Add test sessions
	now := time.Now()
	session1 := &Session{ID: "session-1", WorkspaceID: "ws-1", LastUsedAt: now}
	session2 := &Session{ID: "session-2", WorkspaceID: "ws-2", LastUsedAt: now}
	sessionStore.Add(session1)
	sessionStore.Add(session2)

	// Create request
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	// Execute
	router.Routes().ServeHTTP(w, req)

	// Assert
	require.Equal(t, http.StatusOK, w.Code)

	var response SessionListResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Len(t, response.Sessions, 2)

	// Verify session IDs
	ids := make(map[string]bool)
	for _, session := range response.Sessions {
		ids[session.ID] = true
	}
	require.True(t, ids["session-1"])
	require.True(t, ids["session-2"])
}

func TestSessionRouter_GetSessionByID(t *testing.T) {
	router, sessionStore, _ := setupSessionRouter(t)

	// Add test session
	session := &Session{
		ID:          "session-1",
		WorkspaceID: "ws-1",
		IsAttached:  true,
		IsActive:    true,
		LastUsedAt:  time.Now(),
	}
	sessionStore.Add(session)

	// Create request
	req := httptest.NewRequest("GET", "/session-1", nil)
	w := httptest.NewRecorder()

	// Execute
	router.Routes().ServeHTTP(w, req)

	// Assert
	require.Equal(t, http.StatusOK, w.Code)

	var response SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "session-1", response.ID)
	require.Equal(t, "ws-1", response.WorkspaceID)
	require.True(t, response.IsAttached)
	require.True(t, response.IsActive)
}

func TestSessionRouter_GetSessionByID_NotFound(t *testing.T) {
	router, _, _ := setupSessionRouter(t)

	// Create request
	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()

	// Execute
	router.Routes().ServeHTTP(w, req)

	// Assert
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestSessionRouter_ListSessionsByWorkspace(t *testing.T) {
	router, sessionStore, _ := setupSessionRouter(t)

	// Add test sessions
	now := time.Now()
	session1 := &Session{ID: "session-1", WorkspaceID: "ws-1", LastUsedAt: now}
	session2 := &Session{ID: "session-2", WorkspaceID: "ws-2", LastUsedAt: now}
	session3 := &Session{ID: "session-3", WorkspaceID: "ws-1", LastUsedAt: now}
	sessionStore.Add(session1)
	sessionStore.Add(session2)
	sessionStore.Add(session3)

	// Create request
	req := httptest.NewRequest("GET", "/workspace/ws-1", nil)
	w := httptest.NewRecorder()

	// Execute
	router.Routes().ServeHTTP(w, req)

	// Assert
	require.Equal(t, http.StatusOK, w.Code)

	var response SessionListResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Len(t, response.Sessions, 2)

	// Verify only ws-1 sessions returned
	for _, session := range response.Sessions {
		require.Equal(t, "ws-1", session.WorkspaceID)
	}
}

func TestSessionRouter_CreateSession(t *testing.T) {
	router, sessionStore, _ := setupSessionRouter(t)

	// Create request body
	session := &Session{
		ID:          "session-1",
		WorkspaceID: "ws-1",
		IsAttached:  true,
		IsActive:    true,
		LastUsedAt:  time.Now(),
	}
	body, err := json.Marshal(session)
	require.NoError(t, err)

	// Create request
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Execute
	router.Routes().ServeHTTP(w, req)

	// Assert
	require.Equal(t, http.StatusCreated, w.Code)

	// Verify session was created
	retrieved, err := sessionStore.GetByID("session-1")
	require.NoError(t, err)
	require.Equal(t, "session-1", retrieved.ID)
	require.Equal(t, "ws-1", retrieved.WorkspaceID)
}

func TestSessionRouter_CreateSession_InvalidWorkspace(t *testing.T) {
	router, _, _ := setupSessionRouter(t)

	// Create request body with invalid workspace
	session := &Session{
		ID:          "session-1",
		WorkspaceID: "nonexistent",
		LastUsedAt:  time.Now(),
	}
	body, err := json.Marshal(session)
	require.NoError(t, err)

	// Create request
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Execute
	router.Routes().ServeHTTP(w, req)

	// Assert - should return error
	require.NotEqual(t, http.StatusCreated, w.Code)
}

func TestSessionRouter_UpdateSession(t *testing.T) {
	router, sessionStore, _ := setupSessionRouter(t)

	// Add initial session
	session := &Session{
		ID:          "session-1",
		WorkspaceID: "ws-1",
		IsAttached:  false,
		IsActive:    true,
		LastUsedAt:  time.Now(),
	}
	sessionStore.Add(session)

	// Update session
	session.IsAttached = true
	body, err := json.Marshal(session)
	require.NoError(t, err)

	// Create request
	req := httptest.NewRequest("PUT", "/session-1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Execute
	router.Routes().ServeHTTP(w, req)

	// Assert
	require.Equal(t, http.StatusOK, w.Code)

	// Verify update
	retrieved, err := sessionStore.GetByID("session-1")
	require.NoError(t, err)
	require.True(t, retrieved.IsAttached)
}

func TestSessionRouter_DeleteSession(t *testing.T) {
	router, sessionStore, _ := setupSessionRouter(t)

	// Add test session
	session := &Session{ID: "session-1", WorkspaceID: "ws-1", LastUsedAt: time.Now()}
	sessionStore.Add(session)

	// Create request
	req := httptest.NewRequest("DELETE", "/session-1", nil)
	w := httptest.NewRecorder()

	// Execute
	router.Routes().ServeHTTP(w, req)

	// Assert
	require.Equal(t, http.StatusNoContent, w.Code)

	retrieved, err := sessionStore.GetByID("session-1")
	require.NoError(t, err)
	require.True(t, retrieved.IsDeleted)
	require.NotNil(t, retrieved.Cleanup)
}
