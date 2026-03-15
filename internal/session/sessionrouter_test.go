package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	utmux "github.com/eleonorayaya/utena/internal/tmux"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupSessionRouter(t *testing.T) (*SessionRouter, *SessionStore, *workspace.WorkspaceStore, *mockTmuxClient, uint, uint) {
	t.Helper()

	database := setupTestDB(t)
	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")

	ws1 := &workspace.Workspace{Name: "utena", Path: "/tmp/utena"}
	ws2 := &workspace.Workspace{Name: "other", Path: "/tmp/other"}
	workspaceStore.Add(ws1)
	workspaceStore.Add(ws2)

	mock := newMockTmuxClient()
	tmuxService := utmux.NewTmuxService(mock, bus)
	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	gitService := git.NewGitService()
	service := NewSessionService(sessionStore, workspaceService, gitService, tmuxService, bus, "eqt/")
	controller := NewSessionController(service)
	router := NewSessionRouter(controller)

	return router, sessionStore, workspaceStore, mock, ws1.ID, ws2.ID
}

func TestSessionRouter_ListSessions(t *testing.T) {
	router, sessionStore, _, _, ws1ID, ws2ID := setupSessionRouter(t)

	now := time.Now()
	session1 := &Session{TmuxSessionName: "session-1", WorkspaceID: ws1ID, Status: StatusReady, LastUsedAt: now}
	session2 := &Session{TmuxSessionName: "session-2", WorkspaceID: ws2ID, Status: StatusReady, LastUsedAt: now}
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

	names := make(map[string]bool)
	for _, session := range response.Sessions {
		names[session.TmuxSessionName] = true
	}
	require.True(t, names["session-1"])
	require.True(t, names["session-2"])
}

func TestSessionRouter_GetSessionByID(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)

	session := &Session{
		TmuxSessionName: "session-1",
		WorkspaceID:     ws1ID,
		IsAttached:      true,
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	req := httptest.NewRequest("GET", fmt.Sprintf("/%d", session.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, session.ID, response.ID)
	require.Equal(t, ws1ID, response.WorkspaceID)
	require.True(t, response.IsAttached)
	require.Equal(t, StatusReady, response.Status)
	require.NotNil(t, response.Resources)
	require.Equal(t, ResourceReady, response.Resources.Tmux.Status)
}

func TestSessionRouter_GetSessionByID_NotFound(t *testing.T) {
	router, _, _, _, _, _ := setupSessionRouter(t)

	req := httptest.NewRequest("GET", "/99999", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestSessionRouter_ListSessionsByWorkspace(t *testing.T) {
	router, sessionStore, _, _, ws1ID, ws2ID := setupSessionRouter(t)

	now := time.Now()
	session1 := &Session{TmuxSessionName: "session-1", WorkspaceID: ws1ID, Status: StatusReady, LastUsedAt: now}
	session2 := &Session{TmuxSessionName: "session-2", WorkspaceID: ws2ID, Status: StatusReady, LastUsedAt: now}
	session3 := &Session{TmuxSessionName: "session-3", WorkspaceID: ws1ID, Status: StatusReady, LastUsedAt: now}
	sessionStore.Add(session1)
	sessionStore.Add(session2)
	sessionStore.Add(session3)

	req := httptest.NewRequest("GET", fmt.Sprintf("/workspace/%d", ws1ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response SessionListResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Len(t, response.Sessions, 2)

	for _, session := range response.Sessions {
		require.Equal(t, ws1ID, session.WorkspaceID)
	}
}

func TestSessionRouter_CreateSession(t *testing.T) {
	router, sessionStore, _, tmux, ws1ID, _ := setupSessionRouter(t)

	body := []byte(fmt.Sprintf(`{"name":"session-1","workspace_id":%d}`, ws1ID))

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)

	var resp SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, "utena-session-1", resp.TmuxSessionName)
	require.Equal(t, "session-1", resp.Name)
	require.Equal(t, StatusCreating, resp.Status)

	waitForStatus(t, sessionStore, resp.ID, StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID(resp.ID)
	require.NoError(t, err)
	require.Equal(t, "utena-session-1", retrieved.TmuxSessionName)
	require.Equal(t, ws1ID, retrieved.WorkspaceID)
	require.True(t, tmux.HasSession("utena-session-1"))
}

func TestSessionRouter_CreateSession_WithName(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)

	body := []byte(fmt.Sprintf(`{"name":"main","workspace_id":%d}`, ws1ID))

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)

	var resp SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, "utena-main", resp.TmuxSessionName)
	require.Equal(t, "main", resp.Name)

	waitForStatus(t, sessionStore, resp.ID, StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID(resp.ID)
	require.NoError(t, err)
	require.Equal(t, "main", retrieved.Name)
}

func TestSessionRouter_CreateSession_ExistingBranch(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)

	body := []byte(fmt.Sprintf(`{"workspace_id":%d,"branch":"main","create_worktree":false}`, ws1ID))

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)

	var resp SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, "utena-main", resp.TmuxSessionName)
	require.Equal(t, "main", resp.Name)

	waitForStatus(t, sessionStore, resp.ID, StatusReady, 2*time.Second)

	retrieved, err := sessionStore.GetByID(resp.ID)
	require.NoError(t, err)
	require.Equal(t, "main", retrieved.Name)
	require.Equal(t, "main", retrieved.Branch)
}

func TestSessionRouter_CreateSession_InvalidWorkspace(t *testing.T) {
	router, _, _, _, _, _ := setupSessionRouter(t)

	body := []byte(`{"name":"session-1","workspace_id":99999}`)

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.NotEqual(t, http.StatusAccepted, w.Code)
}

func TestSessionRouter_UpdateSession(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)

	session := &Session{
		TmuxSessionName: "session-1",
		WorkspaceID:     ws1ID,
		IsAttached:      false,
		Status:          StatusReady,
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	session.IsAttached = true
	body, err := json.Marshal(session)
	require.NoError(t, err)

	req := httptest.NewRequest("PUT", fmt.Sprintf("/%d", session.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.True(t, retrieved.IsAttached)
}

func TestSessionRouter_DeleteSession(t *testing.T) {
	router, sessionStore, _, tmux, ws1ID, _ := setupSessionRouter(t)

	tmux.sessions["session-1"] = true
	session := &Session{
		TmuxSessionName: "session-1",
		WorkspaceID:     ws1ID,
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/%d", session.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusDeleted, retrieved.Status)
	require.Equal(t, ResourceRemoved, retrieved.Resources.Tmux.Status)
	require.False(t, tmux.HasSession("session-1"))
}

func TestSessionRouter_RepairSession(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)

	session := &Session{
		TmuxSessionName: "broken-session",
		WorkspaceID:     ws1ID,
		Status:          StatusBroken,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceRemoved}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	req := httptest.NewRequest("PUT", fmt.Sprintf("/%d/repair", session.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, session.ID, response.ID)
	require.Equal(t, StatusCreating, response.Status)

	waitForStatus(t, sessionStore, session.ID, StatusReady, 2*time.Second)
}

func TestSessionRouter_RepairSession_NotFound(t *testing.T) {
	router, _, _, _, _, _ := setupSessionRouter(t)

	req := httptest.NewRequest("PUT", "/99999/repair", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestSessionRouter_RepairSession_NotBroken(t *testing.T) {
	router, sessionStore, _, tmux, ws1ID, _ := setupSessionRouter(t)

	tmux.sessions["alive-session"] = true
	session := &Session{
		TmuxSessionName: "alive-session",
		WorkspaceID:     ws1ID,
		Status:          StatusReady,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceReady}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	req := httptest.NewRequest("PUT", fmt.Sprintf("/%d/repair", session.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}

func TestSessionRouter_ActivateSession_RejectsBrokenSession(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)

	session := &Session{
		TmuxSessionName: "broken-session",
		WorkspaceID:     ws1ID,
		Status:          StatusBroken,
		Resources:       &Resources{Tmux: &ResourceState{Status: ResourceRemoved}},
		LastUsedAt:      time.Now(),
	}
	sessionStore.Add(session)

	req := httptest.NewRequest("PUT", fmt.Sprintf("/%d/activate", session.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSessionRouter_GetSessionByID_ShowsResourceState(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)

	session := &Session{
		TmuxSessionName: "creating-session",
		WorkspaceID:     ws1ID,
		Status:          StatusCreating,
		Resources: &Resources{
			Branch:   &ResourceState{Status: ResourceReady},
			Worktree: &ResourceState{Status: ResourceCreating},
			Tmux:     &ResourceState{Status: ResourcePending},
		},
		LastUsedAt: time.Now(),
	}
	sessionStore.Add(session)

	req := httptest.NewRequest("GET", fmt.Sprintf("/%d", session.ID), nil)
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
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)

	session := &Session{
		TmuxSessionName: "broken-session",
		WorkspaceID:     ws1ID,
		Status:          StatusBroken,
		Resources: &Resources{
			Branch:   &ResourceState{Status: ResourceReady},
			Worktree: &ResourceState{Status: ResourceFailed, Error: "failed to create worktree: timeout"},
			Tmux:     &ResourceState{Status: ResourcePending},
		},
		LastUsedAt: time.Now(),
	}
	sessionStore.Add(session)

	req := httptest.NewRequest("GET", fmt.Sprintf("/%d", session.ID), nil)
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
