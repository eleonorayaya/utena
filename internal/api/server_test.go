package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type testTmuxClient struct {
	mu        sync.Mutex
	sessions  map[string]bool
	createErr error
}

func newTestTmuxClient() *testTmuxClient {
	return &testTmuxClient{sessions: make(map[string]bool)}
}

func (m *testTmuxClient) CreateSession(name, startDir string, env map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return m.createErr
	}
	m.sessions[name] = true
	return nil
}

func (m *testTmuxClient) KillSession(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, name)
	return nil
}

func (m *testTmuxClient) HasSession(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[name]
}

func (m *testTmuxClient) ListSessionNames() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.sessions))
	for name := range m.sessions {
		names = append(names, name)
	}
	return names, nil
}

func (m *testTmuxClient) SwitchClient(targetSession string) error {
	return nil
}

func (m *testTmuxClient) RunCommand(cmd ...string) (string, error) {
	return "", nil
}

func (m *testTmuxClient) setCreateErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createErr = err
}

func setupTestRouter(t *testing.T) (*App, chi.Router, *testTmuxClient, uint, uint) {
	t.Helper()

	gormDB, err := db.OpenInMemorySQLite()
	require.NoError(t, err)

	cfg := Config{ConfigDir: "/config"}
	mock := newTestTmuxClient()
	app := newApp(gormDB, mock, afero.NewMemMapFs(), cfg)

	err = app.OnStart(context.Background())
	require.NoError(t, err)

	ws1 := &workspace.Workspace{Name: "utena", Path: "/tmp/utena"}
	ws2 := &workspace.Workspace{Name: "other", Path: "/tmp/other"}
	app.Workspace.Store.Add(ws1)
	app.Workspace.Store.Add(ws2)

	server := BuildServer(app, cfg)

	return app, server.Handler.(*chi.Mux), mock, ws1.ID, ws2.ID
}

func waitForSessionStatus(t *testing.T, router chi.Router, id uint, status session.SessionStatus, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest("GET", fmt.Sprintf("/sessions/%d", id), nil)
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
	t.Fatalf("session %d did not reach status %q within %v", id, status, timeout)
}

func createSessionViaAPI(t *testing.T, router chi.Router, body string) session.SessionResponse {
	t.Helper()
	req := httptest.NewRequest("POST", "/sessions", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)

	var resp session.SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	return resp
}

func TestDaemon_ListWorkspaces(t *testing.T) {
	_, router, _, ws1ID, ws2ID := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/workspaces", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response workspace.WorkspaceListResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Len(t, response.Workspaces, 2)

	ids := make(map[uint]bool)
	for _, ws := range response.Workspaces {
		ids[ws.ID] = true
	}
	require.True(t, ids[ws1ID])
	require.True(t, ids[ws2ID])
}

func TestDaemon_GetWorkspaceByID(t *testing.T) {
	_, router, _, ws1ID, _ := setupTestRouter(t)

	req := httptest.NewRequest("GET", fmt.Sprintf("/workspaces/%d", ws1ID), nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response workspace.WorkspaceResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, ws1ID, response.ID)
	require.Equal(t, "utena", response.Name)
}

func TestDaemon_CreateAndGetSession(t *testing.T) {
	_, router, _, ws1ID, _ := setupTestRouter(t)

	body := fmt.Sprintf(`{"name":"test-session-1","workspace_id":%d}`, ws1ID)
	createResp := createSessionViaAPI(t, router, body)

	require.Equal(t, "utena-test-session-1", createResp.TmuxSessionName)
	require.Equal(t, "test-session-1", createResp.Name)
	require.Equal(t, session.StatusCreating, createResp.Status)

	waitForSessionStatus(t, router, createResp.ID, session.StatusReady, 2*time.Second)

	req := httptest.NewRequest("GET", fmt.Sprintf("/sessions/%d", createResp.ID), nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response session.SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "utena-test-session-1", response.TmuxSessionName)
	require.Equal(t, ws1ID, response.WorkspaceID)
	require.Equal(t, session.StatusReady, response.Status)
}

func TestDaemon_CreateSession_TmuxFails(t *testing.T) {
	_, router, tmux, ws1ID, _ := setupTestRouter(t)
	tmux.setCreateErr(fmt.Errorf("tmux server not running"))

	body := fmt.Sprintf(`{"name":"fail-session","workspace_id":%d}`, ws1ID)
	createResp := createSessionViaAPI(t, router, body)

	waitForSessionStatus(t, router, createResp.ID, session.StatusBroken, 2*time.Second)

	req := httptest.NewRequest("GET", fmt.Sprintf("/sessions/%d", createResp.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var response session.SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, session.StatusBroken, response.Status)
	require.Equal(t, session.ResourceFailed, response.Resources.Tmux.Status)
	require.Contains(t, response.Resources.Tmux.Error, "tmux server not running")
}

func TestDaemon_ListSessions(t *testing.T) {
	_, router, _, ws1ID, ws2ID := setupTestRouter(t)

	body1 := fmt.Sprintf(`{"name":"session-1","workspace_id":%d}`, ws1ID)
	body2 := fmt.Sprintf(`{"name":"session-2","workspace_id":%d}`, ws2ID)

	resp1 := createSessionViaAPI(t, router, body1)
	resp2 := createSessionViaAPI(t, router, body2)

	waitForSessionStatus(t, router, resp1.ID, session.StatusReady, 2*time.Second)
	waitForSessionStatus(t, router, resp2.ID, session.StatusReady, 2*time.Second)

	req := httptest.NewRequest("GET", "/sessions", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response session.SessionListResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Len(t, response.Sessions, 2)

	names := make(map[string]bool)
	for _, s := range response.Sessions {
		names[s.TmuxSessionName] = true
	}
	require.True(t, names["utena-session-1"])
	require.True(t, names["other-session-2"])
}

func TestDaemon_TmuxHookSessionCreated(t *testing.T) {
	_, router, _, ws1ID, _ := setupTestRouter(t)

	body := fmt.Sprintf(`{"name":"test-session","workspace_id":%d}`, ws1ID)
	createResp := createSessionViaAPI(t, router, body)

	waitForSessionStatus(t, router, createResp.ID, session.StatusReady, 2*time.Second)

	hookBody := []byte(`{"session_name":"utena-test-session"}`)
	req := httptest.NewRequest("PUT", "/tmux/hooks/session-created", bytes.NewReader(hookBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var hookResponse map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &hookResponse)
	require.NoError(t, err)
	require.Equal(t, "ok", hookResponse["status"])

	req = httptest.NewRequest("GET", "/sessions", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var sessionsResponse session.SessionListResponse
	err = json.Unmarshal(w.Body.Bytes(), &sessionsResponse)
	require.NoError(t, err)
	require.Len(t, sessionsResponse.Sessions, 1)

	sess := findSessionByTmuxName(sessionsResponse.Sessions, "utena-test-session")
	require.NotNil(t, sess)
	require.Equal(t, session.StatusReady, sess.Status)
}

func findSessionByTmuxName(sessions []*session.SessionResponse, tmuxName string) *session.SessionResponse {
	for _, s := range sessions {
		if s.TmuxSessionName == tmuxName {
			return s
		}
	}
	return nil
}

func TestDaemon_TmuxHookSessionClosed(t *testing.T) {
	_, router, _, ws1ID, _ := setupTestRouter(t)

	body := fmt.Sprintf(`{"name":"test-session","workspace_id":%d}`, ws1ID)
	createResp := createSessionViaAPI(t, router, body)

	waitForSessionStatus(t, router, createResp.ID, session.StatusReady, 2*time.Second)

	hookBody := []byte(`{"session_name":"utena-test-session"}`)
	req := httptest.NewRequest("PUT", "/tmux/hooks/session-closed", bytes.NewReader(hookBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest("GET", "/sessions", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var sessionsResponse session.SessionListResponse
	err := json.Unmarshal(w.Body.Bytes(), &sessionsResponse)
	require.NoError(t, err)
	require.Len(t, sessionsResponse.Sessions, 1)

	sess := findSessionByTmuxName(sessionsResponse.Sessions, "utena-test-session")
	require.NotNil(t, sess)
	require.Equal(t, session.StatusBroken, sess.Status)
	require.False(t, sess.IsAttached)
}

func TestDaemon_RepairSession_AfterTmuxFailure(t *testing.T) {
	_, router, tmux, ws1ID, _ := setupTestRouter(t)
	tmux.setCreateErr(fmt.Errorf("tmux down"))

	body := fmt.Sprintf(`{"name":"repair-me","workspace_id":%d}`, ws1ID)
	createResp := createSessionViaAPI(t, router, body)

	waitForSessionStatus(t, router, createResp.ID, session.StatusBroken, 2*time.Second)

	tmux.setCreateErr(nil)

	req := httptest.NewRequest("PUT", fmt.Sprintf("/sessions/%d/repair", createResp.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	waitForSessionStatus(t, router, createResp.ID, session.StatusReady, 2*time.Second)

	req = httptest.NewRequest("GET", fmt.Sprintf("/sessions/%d", createResp.ID), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var response session.SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, session.StatusReady, response.Status)
	require.Equal(t, session.ResourceReady, response.Resources.Tmux.Status)
	require.True(t, tmux.HasSession("utena-repair-me"))
}
