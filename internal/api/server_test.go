package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/mux"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func initApiTestRepo(t *testing.T) string {
	t.Helper()
	bareDir := t.TempDir()
	dir := t.TempDir()
	gitEnv := append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	for _, args := range [][]string{
		{"git", "init", "--bare", "-b", "main"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = bareDir
		cmd.Env = gitEnv
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v failed: %s", args, string(out))
	}
	for _, args := range [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "config", "commit.gpgsign", "false"},
		{"git", "config", "tag.gpgsign", "false"},
		{"git", "remote", "add", "origin", bareDir},
		{"git", "commit", "--allow-empty", "-m", "init"},
		{"git", "push", "-u", "origin", "HEAD"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = gitEnv
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v failed: %s", args, string(out))
	}
	return dir
}

func setupTestRouter(t *testing.T) (*App, chi.Router, *mux.FakeDriver, uint, uint) {
	t.Helper()
	app, router, mock, ws1ID, ws2ID, _ := setupTestRouterWithBus(t)
	return app, router, mock, ws1ID, ws2ID
}

func setupTestRouterWithBus(t *testing.T) (*App, chi.Router, *mux.FakeDriver, uint, uint, eventbus.EventBus) {
	t.Helper()

	gormDB, err := db.OpenInMemorySQLite()
	require.NoError(t, err)

	cfg := Config{ConfigDir: "/config", SessionsRoot: t.TempDir(), WM: "none", Browser: "chrome"}
	bus := eventbus.NewEventBus()
	database := db.NewDB(gormDB)
	mock := mux.NewFakeDriver()
	muxModule := mux.NewModule(mock, bus, database)
	gitModule := git.NewGitModule(database, bus)
	app := buildApp(gormDB, afero.NewMemMapFs(), cfg, muxModule, gitModule, bus)

	err = app.OnStart(context.Background())
	require.NoError(t, err)

	repoPath1 := initApiTestRepo(t)
	repoPath2 := initApiTestRepo(t)
	repo1 := &git.Repo{Path: repoPath1, FullName: "test/utena"}
	require.NoError(t, database.Create(repo1).Error)
	repo2 := &git.Repo{Path: repoPath2, FullName: "test/other"}
	require.NoError(t, database.Create(repo2).Error)
	repo1ID := repo1.ID
	repo2ID := repo2.ID
	ws1 := &workspace.Workspace{Name: "utena", Path: repoPath1, IsGitRepo: true, RepoID: &repo1ID}
	ws2 := &workspace.Workspace{Name: "other", Path: repoPath2, IsGitRepo: true, RepoID: &repo2ID}
	require.NoError(t, app.Workspace.Store.Add(ws1))
	require.NoError(t, app.Workspace.Store.Add(ws2))

	server := BuildServer(app, cfg)

	return app, server.Handler.(*chi.Mux), mock, ws1.ID, ws2.ID, bus
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

	body := fmt.Sprintf(`{"name":"test-session-1","workspaces":[{"workspace_id":%d,"base_branch":"main"}]}`, ws1ID)
	createResp := createSessionViaAPI(t, router, body)

	require.Equal(t, "test-session-1", createResp.Name)
	require.Equal(t, session.StatusCreating, createResp.Status)

	waitForSessionStatus(t, router, createResp.ID, session.StatusActive, 5*time.Second)

	req := httptest.NewRequest("GET", fmt.Sprintf("/sessions/%d", createResp.ID), nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response session.SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.NotNil(t, response.MuxSessionID)
	require.Equal(t, session.StatusActive, response.Status)
}

func TestDaemon_CreateSession_MuxFails(t *testing.T) {
	_, router, mock, ws1ID, _ := setupTestRouter(t)
	mock.SetCreateErr(fmt.Errorf("mux server not running"))

	body := fmt.Sprintf(`{"name":"fail-session","workspaces":[{"workspace_id":%d,"base_branch":"main"}]}`, ws1ID)
	createResp := createSessionViaAPI(t, router, body)

	waitForSessionStatus(t, router, createResp.ID, session.StatusBroken, 5*time.Second)

	req := httptest.NewRequest("GET", fmt.Sprintf("/sessions/%d", createResp.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var response session.SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, session.StatusBroken, response.Status)
	require.Contains(t, response.StatusError, "mux")
}

func TestDaemon_ListSessions(t *testing.T) {
	_, router, _, ws1ID, ws2ID := setupTestRouter(t)

	body1 := fmt.Sprintf(`{"name":"session-1","workspaces":[{"workspace_id":%d,"base_branch":"main"}]}`, ws1ID)
	body2 := fmt.Sprintf(`{"name":"session-2","workspaces":[{"workspace_id":%d,"base_branch":"main"}]}`, ws2ID)

	resp1 := createSessionViaAPI(t, router, body1)
	resp2 := createSessionViaAPI(t, router, body2)

	waitForSessionStatus(t, router, resp1.ID, session.StatusActive, 5*time.Second)
	waitForSessionStatus(t, router, resp2.ID, session.StatusActive, 5*time.Second)

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
		names[s.Name] = true
	}
	require.True(t, names["session-1"])
	require.True(t, names["session-2"])
}

func TestDaemon_ListSessions_GzipWhenAccepted(t *testing.T) {
	_, router, _, ws1ID, _ := setupTestRouter(t)

	body := fmt.Sprintf(`{"name":"gzipped","workspaces":[{"workspace_id":%d,"base_branch":"main"}]}`, ws1ID)
	resp := createSessionViaAPI(t, router, body)
	waitForSessionStatus(t, router, resp.ID, session.StatusActive, 5*time.Second)

	req := httptest.NewRequest("GET", "/sessions", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "gzip", w.Header().Get("Content-Encoding"))

	zr, err := gzip.NewReader(w.Body)
	require.NoError(t, err)
	var response session.SessionListResponse
	require.NoError(t, json.NewDecoder(zr).Decode(&response))
	require.Len(t, response.Sessions, 1)
	require.Equal(t, "gzipped", response.Sessions[0].Name)
}

func TestDaemon_TuiosSessionCreated(t *testing.T) {
	app, router, _, ws1ID, _ := setupTestRouter(t)

	body := fmt.Sprintf(`{"name":"test-session","workspaces":[{"workspace_id":%d,"base_branch":"main"}]}`, ws1ID)
	createResp := createSessionViaAPI(t, router, body)

	waitForSessionStatus(t, router, createResp.ID, session.StatusActive, 5*time.Second)

	require.NoError(t, app.Mux.Service.HandleSessionCreated(context.Background(), "test-session"))

	req := httptest.NewRequest("GET", "/sessions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var sessionsResponse session.SessionListResponse
	err := json.Unmarshal(w.Body.Bytes(), &sessionsResponse)
	require.NoError(t, err)
	require.Len(t, sessionsResponse.Sessions, 1)

	sess := findSessionByMuxName(sessionsResponse.Sessions, "test-session")
	require.NotNil(t, sess)
	require.Equal(t, session.StatusActive, sess.Status)
}

func findSessionByMuxName(sessions []*session.SessionResponse, muxName string) *session.SessionResponse {
	for _, s := range sessions {
		if s.MuxSession != nil && s.MuxSession.Name == muxName {
			return s
		}
	}
	return nil
}

func TestDaemon_TuiosSessionClosed(t *testing.T) {
	app, router, _, ws1ID, _ := setupTestRouter(t)

	body := fmt.Sprintf(`{"name":"test-session","workspaces":[{"workspace_id":%d,"base_branch":"main"}]}`, ws1ID)
	createResp := createSessionViaAPI(t, router, body)

	waitForSessionStatus(t, router, createResp.ID, session.StatusActive, 5*time.Second)

	require.NoError(t, app.Mux.Service.HandleSessionClosed(context.Background(), "test-session"))

	req := httptest.NewRequest("GET", "/sessions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var sessionsResponse session.SessionListResponse
	err := json.Unmarshal(w.Body.Bytes(), &sessionsResponse)
	require.NoError(t, err)
	require.Len(t, sessionsResponse.Sessions, 1)

	sess := findSessionByMuxName(sessionsResponse.Sessions, "test-session")
	require.NotNil(t, sess)
	require.Equal(t, session.StatusBroken, sess.Status)
	require.False(t, sess.IsAttached)
}

func TestDaemon_RepairSession_AfterMuxFailure(t *testing.T) {
	_, router, mock, ws1ID, _ := setupTestRouter(t)
	mock.SetCreateErr(fmt.Errorf("mux down"))

	body := fmt.Sprintf(`{"name":"repair-me","workspaces":[{"workspace_id":%d,"base_branch":"main"}]}`, ws1ID)
	createResp := createSessionViaAPI(t, router, body)

	waitForSessionStatus(t, router, createResp.ID, session.StatusBroken, 5*time.Second)

	mock.SetCreateErr(nil)

	req := httptest.NewRequest("PUT", fmt.Sprintf("/sessions/%d/repair", createResp.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	waitForSessionStatus(t, router, createResp.ID, session.StatusActive, 5*time.Second)

	req = httptest.NewRequest("GET", fmt.Sprintf("/sessions/%d", createResp.ID), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var response session.SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, session.StatusActive, response.Status)
	require.True(t, mock.HasSession("repair-me"))
}

func startMigrateAndWait(t *testing.T, router http.Handler, wsID uint) {
	t.Helper()
	req := httptest.NewRequest("POST", fmt.Sprintf("/workspaces/%d/migrate-bare", wsID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code, "body: %s", w.Body.String())

	var started struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &started))
	require.Equal(t, "migrating", started.Status)

	require.Eventually(t, func() bool {
		sreq := httptest.NewRequest("GET", fmt.Sprintf("/workspaces/%d", wsID), nil)
		sw := httptest.NewRecorder()
		router.ServeHTTP(sw, sreq)
		if sw.Code != http.StatusOK {
			return false
		}
		var status struct {
			Status      string `json:"status"`
			StatusError string `json:"status_error"`
		}
		require.NoError(t, json.Unmarshal(sw.Body.Bytes(), &status))
		require.NotEqual(t, "failed", status.Status, "migration failed: %s", status.StatusError)
		return status.Status == "ready"
	}, 30*time.Second, 25*time.Millisecond)
}

func TestDaemon_MigrateToBare_BootstrapsClaudeSettings(t *testing.T) {
	app, router, _, ws1ID, _ := setupTestRouter(t)

	startMigrateAndWait(t, router, ws1ID)

	ws, err := app.Workspace.Store.GetByID(ws1ID)
	require.NoError(t, err)
	require.True(t, ws.IsBare, "workspace should be marked bare after migration")

	settingsPath := filepath.Join(ws.Path, ".claude", "settings.local.json")
	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err)
	require.NotEmpty(t, data)
}

func TestDaemon_MigrateToBare_DetachesLinkedSessions(t *testing.T) {
	app, router, _, ws1ID, _ := setupTestRouter(t)

	body := fmt.Sprintf(`{"name":"linked","workspaces":[{"workspace_id":%d,"base_branch":"main"}]}`, ws1ID)
	createResp := createSessionViaAPI(t, router, body)
	waitForSessionStatus(t, router, createResp.ID, session.StatusActive, 5*time.Second)

	startMigrateAndWait(t, router, ws1ID)

	sess, err := app.Session.Store.GetByID(createResp.ID)
	require.NoError(t, err)
	require.Empty(t, sess.Worktrees, "migrated repo should leave the session with no remaining worktrees")
}

func TestDaemon_MuxRoutes(t *testing.T) {
	_, router, _, _, _, bus := setupTestRouterWithBus(t)

	var published []eventbus.Event
	bus.Subscribe(eventbus.MuxClientAttached, func(ctx context.Context, e eventbus.Event) error {
		published = append(published, e)
		return nil
	})

	tests := []struct {
		method   string
		path     string
		body     string
		wantCode int
		wantBody func(t *testing.T, body string)
	}{
		{"PUT", "/mux/hooks/client-attached", `{"session_name":"x"}`, http.StatusOK, nil},
		{"GET", "/mux/sessions/nope/env/K", "", http.StatusNotFound, func(t *testing.T, body string) {
			require.Empty(t, body)
		}},
		{"PUT", "/tmux/hooks/client-attached", `{"session_name":"x"}`, http.StatusNotFound, func(t *testing.T, body string) {
			require.Contains(t, body, "404 page not found")
		}},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader([]byte(tt.body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, tt.wantCode, w.Code, "%s %s", tt.method, tt.path)
		if tt.wantBody != nil {
			tt.wantBody(t, w.Body.String())
		}
	}

	require.Len(t, published, 1)
	require.Equal(t, eventbus.MuxHookEvent{SessionName: "x"}, published[0].Data)
}
