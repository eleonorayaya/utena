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
	"github.com/eleonorayaya/utena/internal/mux"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupSessionRouter(t *testing.T) (*SessionRouter, *SessionStore, *workspace.WorkspaceStore, *mux.FakeDriver, uint, uint) {
	t.Helper()

	database := setupTestDB(t)
	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")

	ws1 := &workspace.Workspace{Name: "utena", Path: "/tmp/utena"}
	ws2 := &workspace.Workspace{Name: "other", Path: "/tmp/other"}
	require.NoError(t, workspaceStore.Add(ws1))
	require.NoError(t, workspaceStore.Add(ws2))

	mock := mux.NewFakeDriver()
	muxService := createMuxService(t, database, mock, bus)
	gitService := git.NewGitService(database)
	workspaceService := workspace.NewWorkspaceService(workspaceStore, gitService)
	dismissedPRStore := NewDismissedPRStore(database)
	sessionActionStore := NewSessionActionStore(database)
	sessionWorktreeStore := NewSessionWorktreeStore(database)
	service := NewSessionService(sessionStore, sessionWorktreeStore, dismissedPRStore, sessionActionStore, NewSessionSetupStepStore(database), workspaceService, gitService, muxService, bus, "eqt/", t.TempDir(), t.TempDir())
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

func TestSessionRouter_ListSessions_StatusFilter(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)
	swStore := NewSessionWorktreeStore(sessionStore.db)

	now := time.Now()
	addTestSession(t, sessionStore, swStore, &Session{Name: "live", Status: StatusActive, LastUsedAt: now}, ws1ID)
	addTestSession(t, sessionStore, swStore, &Session{Name: "finished", Status: StatusCompleted, LastUsedAt: now}, ws1ID)
	addTestSession(t, sessionStore, swStore, &Session{Name: "old", Status: StatusArchived, LastUsedAt: now}, ws1ID)

	for _, query := range []string{"?status=active,completed", "?status=active&status=completed"} {
		req := httptest.NewRequest("GET", "/"+query, nil)
		w := httptest.NewRecorder()
		router.Routes().ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var response SessionListResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		names := []string{}
		for _, s := range response.Sessions {
			names = append(names, s.Name)
		}
		require.ElementsMatch(t, []string{"live", "finished"}, names, query)
	}
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

	body := []byte(fmt.Sprintf(`{"name":"session-1","workspaces":[{"workspace_id":%d}]}`, ws1ID))

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSessionRouter_CreateSession_InvalidWorkspace(t *testing.T) {
	router, _, _, _, _, _ := setupSessionRouter(t)

	body := []byte(`{"name":"session-1","workspaces":[{"workspace_id":99999,"branch":"main"}]}`)

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
		Status:     StatusArchived,
		LastUsedAt: time.Now(),
	}
	addTestSession(t, sessionStore, swStore, session, ws1ID)

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/%d", session.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)

	_, err := sessionStore.GetByID(session.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSessionRouter_RepairSession_NotFound(t *testing.T) {
	router, _, _, _, _, _ := setupSessionRouter(t)

	req := httptest.NewRequest("PUT", "/99999/repair", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestSessionRouter_RepairSession_NotBroken(t *testing.T) {
	router, sessionStore, _, muxDriver, _, _ := setupSessionRouter(t)

	require.NoError(t, muxDriver.NewSession("utena-alive-session", "", nil))
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

func TestSessionRouter_AddWorkspace_RejectsNoWorkspaceID(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)
	swStore := NewSessionWorktreeStore(sessionStore.db)

	session := &Session{Name: "session-1", Status: StatusActive, LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, swStore, session, ws1ID)

	body := []byte(`{"branch":"main"}`)
	req := httptest.NewRequest("POST", fmt.Sprintf("/%d/workspaces", session.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSessionRouter_AddWorkspace_RejectsBothBranchFields(t *testing.T) {
	router, sessionStore, _, _, ws1ID, ws2ID := setupSessionRouter(t)
	swStore := NewSessionWorktreeStore(sessionStore.db)

	session := &Session{Name: "session-1", Status: StatusActive, LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, swStore, session, ws1ID)

	body := []byte(fmt.Sprintf(`{"workspace_id":%d,"branch":"main","base_branch":"main"}`, ws2ID))
	req := httptest.NewRequest("POST", fmt.Sprintf("/%d/workspaces", session.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSessionRouter_AddWorkspace_SessionNotFound(t *testing.T) {
	router, _, _, _, _, ws2ID := setupSessionRouter(t)

	body := []byte(fmt.Sprintf(`{"workspace_id":%d,"branch":"main"}`, ws2ID))
	req := httptest.NewRequest("POST", "/99999/workspaces", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestSessionRouter_AddWorkspace_DeletedSession(t *testing.T) {
	router, sessionStore, _, _, ws1ID, ws2ID := setupSessionRouter(t)
	swStore := NewSessionWorktreeStore(sessionStore.db)

	session := &Session{Name: "deleted-session", Status: StatusDeleted, LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, swStore, session, ws1ID)

	body := []byte(fmt.Sprintf(`{"workspace_id":%d,"branch":"main"}`, ws2ID))
	req := httptest.NewRequest("POST", fmt.Sprintf("/%d/workspaces", session.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
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

func TestSessionRouter_BranchHook_UnknownPathReturnsNoContent(t *testing.T) {
	router, _, _, _, _, _ := setupSessionRouter(t)

	body := bytes.NewBufferString(`{"path":"/not/a/session","branch":"eqt/x"}`)
	req := httptest.NewRequest(http.MethodPut, "/branch-hook", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestSessionRouter_BranchHook_RequiresPath(t *testing.T) {
	router, _, _, _, _, _ := setupSessionRouter(t)

	body := bytes.NewBufferString(`{"branch":"eqt/x"}`)
	req := httptest.NewRequest(http.MethodPut, "/branch-hook", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSessionRouter_ListResources(t *testing.T) {
	router, sessionStore, _, driver, ws1ID, _ := setupSessionRouter(t)
	swStore := NewSessionWorktreeStore(sessionStore.db)

	muxSession := &mux.Session{Name: "utena-alpha", Status: mux.StatusActive}
	require.NoError(t, mux.NewStore(sessionStore.db).Add(muxSession))
	alpha := &Session{Name: "alpha", Status: StatusActive, LastUsedAt: time.Now(), MuxSessionID: &muxSession.ID}
	addTestSession(t, sessionStore, swStore, alpha, ws1ID)

	driver.SetPaneShell(11, "utena-alpha")
	driver.SetPaneShell(21, "scratch")
	lister := &mux.FakeProcessLister{}
	lister.SetProcesses([]mux.Process{
		{PID: 10, PPID: 1, Command: "tuios", RSSBytes: 9999},
		{PID: 11, PPID: 10, RSSBytes: 1024, CPUPercent: 1},
		{PID: 12, PPID: 11, RSSBytes: 4096, CPUPercent: 40},
		{PID: 21, PPID: 10, RSSBytes: 2048, CPUPercent: 3},
	})
	router.controller.service.muxService.SetProcessLister(lister)

	req := httptest.NewRequest("GET", "/resources", nil)
	w := httptest.NewRecorder()
	router.Routes().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp SessionResourcesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Sessions, 2)
	require.False(t, resp.SampledAt.IsZero())

	scratch, managed := resp.Sessions[0], resp.Sessions[1]
	require.Equal(t, SessionResources{
		Name: "scratch", MuxSession: "scratch", Status: ResourceStatusUnmanaged, CPUPercent: 3, RSSBytes: 2048, Procs: 1,
		Processes: []mux.ProcessUsage{{PID: 21, Command: "?", CPUPercent: 3, RSSBytes: 2048}},
	}, scratch)
	require.NotNil(t, managed.SessionID)
	require.Equal(t, alpha.ID, *managed.SessionID)
	require.Equal(t, "alpha", managed.Name)
	require.Equal(t, "utena-alpha", managed.MuxSession)
	require.Equal(t, string(StatusActive), managed.Status)
	require.InDelta(t, 41, managed.CPUPercent, 0.001)
	require.Equal(t, int64(5120), managed.RSSBytes)
	require.Equal(t, 2, managed.Procs)
	require.Equal(t, []int{11, 12}, []int{managed.Processes[0].PID, managed.Processes[1].PID})
}

func TestSessionRouter_ListResources_DeletedSessionIsUnmanaged(t *testing.T) {
	router, sessionStore, _, driver, ws1ID, _ := setupSessionRouter(t)
	swStore := NewSessionWorktreeStore(sessionStore.db)

	muxSession := &mux.Session{Name: "utena-gone", Status: mux.StatusActive}
	require.NoError(t, mux.NewStore(sessionStore.db).Add(muxSession))
	gone := &Session{Name: "gone", Status: StatusDeleted, LastUsedAt: time.Now(), MuxSessionID: &muxSession.ID}
	addTestSession(t, sessionStore, swStore, gone, ws1ID)

	driver.SetPaneShell(11, "utena-gone")
	lister := &mux.FakeProcessLister{}
	lister.SetProcesses([]mux.Process{{PID: 10, PPID: 1, Command: "tuios"}, {PID: 11, PPID: 10, Command: "/bin/zsh"}})
	router.controller.service.muxService.SetProcessLister(lister)

	w := httptest.NewRecorder()
	router.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/resources", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp SessionResourcesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Sessions, 1)
	require.Nil(t, resp.Sessions[0].SessionID)
	require.Equal(t, ResourceStatusUnmanaged, resp.Sessions[0].Status)
	require.Equal(t, "utena-gone", resp.Sessions[0].Name)
}
