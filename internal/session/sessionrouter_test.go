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

func setupSessionRouter(t *testing.T) (*SessionRouter, *SessionStore, *workspace.WorkspaceStore, *mockTmuxManager) {
	t.Helper()

	database := setupTestDB(t)
	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")

	workspaceStore.Add(&workspace.Workspace{ID: "ws-1", Name: "utena", Path: "/tmp/utena"})
	workspaceStore.Add(&workspace.Workspace{ID: "ws-2", Name: "other", Path: "/tmp/other"})

	tmux := newMockTmuxManager()
	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	gitService := git.NewGitService()
	service := NewSessionService(sessionStore, workspaceService, gitService, tmux, bus, "eqt/")
	controller := NewSessionController(service)
	router := NewSessionRouter(controller)

	return router, sessionStore, workspaceStore, tmux
}

func TestSessionRouter_ListSessions(t *testing.T) {
	router, sessionStore, _, _ := setupSessionRouter(t)

	now := time.Now()
	session1 := &Session{ID: "session-1", TmuxSessionName: "session-1", WorkspaceID: "ws-1", Status: StatusReady, LastUsedAt: now}
	session2 := &Session{ID: "session-2", TmuxSessionName: "session-2", WorkspaceID: "ws-2", Status: StatusReady, LastUsedAt: now}
	sessionStore.Add(session1)
	sessionStore.Add(session2)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response SessionListResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Len(t, response.Sessions, 2)

	ids := make(map[string]bool)
	for _, session := range response.Sessions {
		ids[session.ID] = true
	}
	require.True(t, ids["session-1"])
	require.True(t, ids["session-2"])
}

func TestSessionRouter_GetSessionByID(t *testing.T) {
	router, sessionStore, _, _ := setupSessionRouter(t)

	session := &Session{
		ID:              "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     "ws-1",
		IsAttached:      true,
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	req := httptest.NewRequest("GET", "/session-1", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "session-1", response.ID)
	require.Equal(t, "ws-1", response.WorkspaceID)
	require.True(t, response.IsAttached)
	require.Equal(t, StatusReady, response.Status)
	require.NotNil(t, response.Resources)
	require.Equal(t, ResourceReady, response.Resources.Tmux.Status)
}

func TestSessionRouter_GetSessionByID_NotFound(t *testing.T) {
	router, _, _, _ := setupSessionRouter(t)

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestSessionRouter_ListSessionsByWorkspace(t *testing.T) {
	router, sessionStore, _, _ := setupSessionRouter(t)

	now := time.Now()
	session1 := &Session{ID: "session-1", TmuxSessionName: "session-1", WorkspaceID: "ws-1", Status: StatusReady, LastUsedAt: now}
	session2 := &Session{ID: "session-2", TmuxSessionName: "session-2", WorkspaceID: "ws-2", Status: StatusReady, LastUsedAt: now}
	session3 := &Session{ID: "session-3", TmuxSessionName: "session-3", WorkspaceID: "ws-1", Status: StatusReady, LastUsedAt: now}
	sessionStore.Add(session1)
	sessionStore.Add(session2)
	sessionStore.Add(session3)

	req := httptest.NewRequest("GET", "/workspace/ws-1", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response SessionListResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Len(t, response.Sessions, 2)

	for _, session := range response.Sessions {
		require.Equal(t, "ws-1", session.WorkspaceID)
	}
}

func TestSessionRouter_CreateSession(t *testing.T) {
	router, sessionStore, _, tmux := setupSessionRouter(t)

	body := []byte(`{"name":"session-1","workspace_id":"ws-1"}`)

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)

	var resp SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, "utena-session-1", resp.ID)
	require.Equal(t, "session-1", resp.Name)
	require.Equal(t, StatusCreating, resp.Status)

	waitForStatus(t, sessionStore, "utena-session-1", StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID("utena-session-1")
	require.NoError(t, err)
	require.Equal(t, "utena-session-1", retrieved.ID)
	require.Equal(t, "ws-1", retrieved.WorkspaceID)
	require.True(t, tmux.HasSession("utena-session-1"))
}

func TestSessionRouter_CreateSession_WithName(t *testing.T) {
	router, sessionStore, _, _ := setupSessionRouter(t)

	body := []byte(`{"name":"main","workspace_id":"ws-1"}`)

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)

	var resp SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, "utena-main", resp.ID)
	require.Equal(t, "main", resp.Name)

	waitForStatus(t, sessionStore, "utena-main", StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID("utena-main")
	require.NoError(t, err)
	require.Equal(t, "main", retrieved.Name)
}

func TestSessionRouter_CreateSession_ExistingBranch(t *testing.T) {
	router, sessionStore, _, _ := setupSessionRouter(t)

	body := []byte(`{"workspace_id":"ws-1","branch":"main","branch_created":false,"create_worktree":false}`)

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)

	var resp SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, "utena-main", resp.ID)
	require.Equal(t, "main", resp.Name)

	waitForStatus(t, sessionStore, "utena-main", StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID("utena-main")
	require.NoError(t, err)
	require.Equal(t, "main", retrieved.Name)
	require.Equal(t, "main", retrieved.Branch)
}

func TestSessionRouter_CreateSession_InvalidWorkspace(t *testing.T) {
	router, _, _, _ := setupSessionRouter(t)

	body := []byte(`{"name":"session-1","workspace_id":"nonexistent"}`)

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.NotEqual(t, http.StatusAccepted, w.Code)
}

func TestSessionRouter_UpdateSession(t *testing.T) {
	router, sessionStore, _, _ := setupSessionRouter(t)

	session := &Session{
		ID:              "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     "ws-1",
		IsAttached:      false,
		Status:          StatusReady,
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	session.IsAttached = true
	body, err := json.Marshal(session)
	require.NoError(t, err)

	req := httptest.NewRequest("PUT", "/session-1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	retrieved, err := sessionStore.GetByID("session-1")
	require.NoError(t, err)
	require.True(t, retrieved.IsAttached)
}

func TestSessionRouter_DeleteSession(t *testing.T) {
	router, sessionStore, _, tmux := setupSessionRouter(t)

	tmux.sessions["session-1"] = true
	session := &Session{
		ID:              "session-1",
		TmuxSessionName: "session-1",
		WorkspaceID:     "ws-1",
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	req := httptest.NewRequest("DELETE", "/session-1", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)

	retrieved, err := sessionStore.GetByID("session-1")
	require.NoError(t, err)
	require.Equal(t, StatusDeleted, retrieved.Status)
	require.Equal(t, ResourceRemoved, retrieved.Resources.Tmux.Status)
	require.False(t, tmux.HasSession("session-1"))
}

func TestSessionRouter_RepairSession(t *testing.T) {
	router, sessionStore, _, _ := setupSessionRouter(t)

	sessionStore.Add(&Session{
		ID:              "broken-session",
		TmuxSessionName: "broken-session",
		WorkspaceID:     "ws-1",
		Status:          StatusBroken,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceRemoved}},
		LastUsedAt:      time.Now(),
	})

	req := httptest.NewRequest("PUT", "/broken-session/repair", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "broken-session", response.ID)
	require.Equal(t, StatusCreating, response.Status)

	waitForStatus(t, sessionStore, "broken-session", StatusReady, 2*time.Second)
}

func TestSessionRouter_RepairSession_NotFound(t *testing.T) {
	router, _, _, _ := setupSessionRouter(t)

	req := httptest.NewRequest("PUT", "/nonexistent/repair", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestSessionRouter_RepairSession_NotBroken(t *testing.T) {
	router, sessionStore, _, tmux := setupSessionRouter(t)

	tmux.sessions["alive-session"] = true
	sessionStore.Add(&Session{
		ID:              "alive-session",
		TmuxSessionName: "alive-session",
		WorkspaceID:     "ws-1",
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	})

	req := httptest.NewRequest("PUT", "/alive-session/repair", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}

func TestSessionRouter_ActivateSession_RejectsBrokenSession(t *testing.T) {
	router, sessionStore, _, _ := setupSessionRouter(t)

	sessionStore.Add(&Session{
		ID:              "broken-session",
		TmuxSessionName: "broken-session",
		WorkspaceID:     "ws-1",
		Status:          StatusBroken,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceRemoved}},
		LastUsedAt:      time.Now(),
	})

	req := httptest.NewRequest("PUT", "/broken-session/activate", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSessionRouter_GetSessionByID_ShowsResourceState(t *testing.T) {
	router, sessionStore, _, _ := setupSessionRouter(t)

	session := &Session{
		ID:              "creating-session",
		TmuxSessionName: "creating-session",
		WorkspaceID:     "ws-1",
		Status:          StatusCreating,
		Resources: &Resources{
			Branch:   &ResourceState{Status: ResourceReady},
			Worktree: &ResourceState{Status: ResourceCreating},
			Tmux:     &ResourceState{Status: ResourcePending},
		},
		LastUsedAt: time.Now(),
	}
	sessionStore.Add(session)

	req := httptest.NewRequest("GET", "/creating-session", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, StatusCreating, response.Status)
	require.Equal(t, ResourceReady, response.Resources.Branch.Status)
	require.Equal(t, ResourceCreating, response.Resources.Worktree.Status)
	require.Equal(t, ResourcePending, response.Resources.Tmux.Status)
}

func TestSessionRouter_GetSessionByID_ShowsBrokenResourceError(t *testing.T) {
	router, sessionStore, _, _ := setupSessionRouter(t)

	session := &Session{
		ID:              "broken-session",
		TmuxSessionName: "broken-session",
		WorkspaceID:     "ws-1",
		Status:          StatusBroken,
		Resources: &Resources{
			Branch:   &ResourceState{Status: ResourceReady},
			Worktree: &ResourceState{Status: ResourceFailed, Error: "failed to create worktree: timeout"},
			Tmux:     &ResourceState{Status: ResourcePending},
		},
		LastUsedAt: time.Now(),
	}
	sessionStore.Add(session)

	req := httptest.NewRequest("GET", "/broken-session", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, StatusBroken, response.Status)
	require.Equal(t, ResourceFailed, response.Resources.Worktree.Status)
	require.Equal(t, "failed to create worktree: timeout", response.Resources.Worktree.Error)
}
