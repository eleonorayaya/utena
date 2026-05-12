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

func setupSessionRouter(t *testing.T) (*SessionRouter, *SessionStore, *workspace.WorkspaceStore, *utmux.MockRunner, uint, uint) {
	t.Helper()

	database := setupTestDB(t)
	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")

	ws1 := &workspace.Workspace{Name: "utena", Path: "/tmp/utena"}
	ws2 := &workspace.Workspace{Name: "other", Path: "/tmp/other"}
	require.NoError(t, workspaceStore.Add(ws1))
	require.NoError(t, workspaceStore.Add(ws2))

	mock := utmux.NewMockRunner()
	tmuxService := createTmuxService(t, database, mock, bus)
	gitService := git.NewGitService(database)
	workspaceService := workspace.NewWorkspaceService(workspaceStore, gitService)
	dismissedPRStore := NewDismissedPRStore(database)
	sessionActionStore := NewSessionActionStore(database)
	sessionWorktreeStore := NewSessionWorktreeStore(database)
	service := NewSessionService(sessionStore, sessionWorktreeStore, dismissedPRStore, sessionActionStore, workspaceService, gitService, tmuxService, bus, "eqt/", t.TempDir(), t.TempDir())
	controller := NewSessionController(service)
	router := NewSessionRouter(controller)

	return router, sessionStore, workspaceStore, mock, ws1.ID, ws2.ID
}

func TestSessionRouter_ListSessions(t *testing.T) {
	router, sessionStore, _, _, ws1ID, ws2ID := setupSessionRouter(t)
	swStore := NewSessionWorktreeStore(sessionStore.db)

	now := time.Now()
	session1 := &Session{Name: "session-1", Status: StatusActive, LastUsedAt: now}
	session2 := &Session{Name: "session-2", Status: StatusActive, LastUsedAt: now}
	addTestSession(t, sessionStore, swStore, session1, ws1ID)
	addTestSession(t, sessionStore, swStore, session2, ws2ID)

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
		names[session.Name] = true
	}
	require.True(t, names["session-1"])
	require.True(t, names["session-2"])
}

func TestSessionRouter_GetSessionByID(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)
	swStore := NewSessionWorktreeStore(sessionStore.db)

	session := &Session{
		Name:       "session-1",
		IsAttached: true,
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swStore, session, ws1ID)

	req := httptest.NewRequest("GET", fmt.Sprintf("/%d", session.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, session.ID, response.ID)
	require.True(t, response.IsAttached)
	require.Equal(t, StatusActive, response.Status)
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
	swStore := NewSessionWorktreeStore(sessionStore.db)

	now := time.Now()
	session1 := &Session{Name: "session-1", Status: StatusActive, LastUsedAt: now}
	session2 := &Session{Name: "session-2", Status: StatusActive, LastUsedAt: now}
	session3 := &Session{Name: "session-3", Status: StatusActive, LastUsedAt: now}
	addTestSession(t, sessionStore, swStore, session1, ws1ID)
	addTestSession(t, sessionStore, swStore, session2, ws2ID)
	addTestSession(t, sessionStore, swStore, session3, ws1ID)

	req := httptest.NewRequest("GET", fmt.Sprintf("/workspace/%d", ws1ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response SessionListResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Len(t, response.Sessions, 2)
}

func TestSessionRouter_CreateSession_RejectsNoBranch(t *testing.T) {
	router, _, _, _, ws1ID, _ := setupSessionRouter(t)

	body := []byte(fmt.Sprintf(`{"name":"session-1","workspace_ids":[%d]}`, ws1ID))

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSessionRouter_CreateSession_InvalidWorkspace(t *testing.T) {
	router, _, _, _, _, _ := setupSessionRouter(t)

	body := []byte(`{"name":"session-1","workspace_ids":[99999],"branch":"main"}`)

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.NotEqual(t, http.StatusAccepted, w.Code)
}

func TestSessionRouter_UpdateSession(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)
	swStore := NewSessionWorktreeStore(sessionStore.db)

	session := &Session{
		Name:       "session-1",
		IsAttached: false,
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swStore, session, ws1ID)

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
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)
	swStore := NewSessionWorktreeStore(sessionStore.db)

	session := &Session{
		Name:       "session-1",
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swStore, session, ws1ID)

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/%d", session.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, StatusDeleted, retrieved.Status)
}

func TestSessionRouter_RepairSession_NotFound(t *testing.T) {
	router, _, _, _, _, _ := setupSessionRouter(t)

	req := httptest.NewRequest("PUT", "/99999/repair", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestSessionRouter_RepairSession_NotBroken(t *testing.T) {
	router, sessionStore, _, tmux, _, _ := setupSessionRouter(t)

	tmux.Sessions["utena-alive-session"] = true
	session := &Session{
		Name:       "alive-session",
		Status:     StatusActive,
		LastUsedAt: time.Now(),
	}
	require.NoError(t, sessionStore.Add(session))

	req := httptest.NewRequest("PUT", fmt.Sprintf("/%d/repair", session.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSessionRouter_ActivateSession_RejectsBrokenSession(t *testing.T) {
	router, sessionStore, _, _, _, _ := setupSessionRouter(t)

	session := &Session{
		Name:       "broken-session",
		Status:     StatusBroken,
		LastUsedAt: time.Now(),
	}
	require.NoError(t, sessionStore.Add(session))

	req := httptest.NewRequest("PUT", fmt.Sprintf("/%d/activate", session.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSessionRouter_GetSessionByID_ShowsCreatingStatus(t *testing.T) {
	router, sessionStore, _, _, _, _ := setupSessionRouter(t)

	session := &Session{
		Name:       "creating-session",
		Status:     StatusCreating,
		LastUsedAt: time.Now(),
	}
	require.NoError(t, sessionStore.Add(session))

	req := httptest.NewRequest("GET", fmt.Sprintf("/%d", session.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, StatusCreating, response.Status)
}

func TestSessionRouter_GetSessionByID_ShowsBrokenStatusError(t *testing.T) {
	router, sessionStore, _, _, _, _ := setupSessionRouter(t)

	session := &Session{
		Name:        "broken-session",
		Status:      StatusBroken,
		StatusError: "worktree setup failed: timeout",
		LastUsedAt:  time.Now(),
	}
	require.NoError(t, sessionStore.Add(session))

	req := httptest.NewRequest("GET", fmt.Sprintf("/%d", session.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, StatusBroken, response.Status)
	require.Equal(t, "worktree setup failed: timeout", response.StatusError)
}
