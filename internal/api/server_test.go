package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tmux"
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

func setupTestRouter(t *testing.T) (*App, chi.Router, *tmux.MockRunner, uint, uint) {
	t.Helper()

	gormDB, err := db.OpenInMemorySQLite()
	require.NoError(t, err)

	cfg := Config{ConfigDir: "/config"}
	bus := eventbus.NewEventBus()
	database := db.NewDB(gormDB)
	mock := tmux.NewMockRunner()
	tmuxStore := tmux.NewTmuxStore(database)
	tmuxModule := tmux.NewTmuxModuleWithRunner(mock, tmuxStore, bus)
	gitModule := git.NewGitModule(database, bus)
	app := buildApp(gormDB, afero.NewMemMapFs(), cfg, tmuxModule, gitModule, bus)

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

	body := fmt.Sprintf(`{"name":"test-session-1","workspace_ids":[%d],"base_branch":"main"}`, ws1ID)
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
	require.NotNil(t, response.TmuxSessionID)
	require.Equal(t, session.StatusActive, response.Status)
}

func TestDaemon_CreateSession_TmuxFails(t *testing.T) {
	_, router, mock, ws1ID, _ := setupTestRouter(t)
	mock.SetCreateErr(fmt.Errorf("tmux server not running"))

	body := fmt.Sprintf(`{"name":"fail-session","workspace_ids":[%d],"base_branch":"main"}`, ws1ID)
	createResp := createSessionViaAPI(t, router, body)

	waitForSessionStatus(t, router, createResp.ID, session.StatusBroken, 5*time.Second)

	req := httptest.NewRequest("GET", fmt.Sprintf("/sessions/%d", createResp.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var response session.SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, session.StatusBroken, response.Status)
	require.Contains(t, response.StatusError, "tmux")
}

func TestDaemon_ListSessions(t *testing.T) {
	_, router, _, ws1ID, ws2ID := setupTestRouter(t)

	body1 := fmt.Sprintf(`{"name":"session-1","workspace_ids":[%d],"base_branch":"main"}`, ws1ID)
	body2 := fmt.Sprintf(`{"name":"session-2","workspace_ids":[%d],"base_branch":"main"}`, ws2ID)

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

func TestDaemon_TmuxHookSessionCreated(t *testing.T) {
	_, router, _, ws1ID, _ := setupTestRouter(t)

	body := fmt.Sprintf(`{"name":"test-session","workspace_ids":[%d],"base_branch":"main"}`, ws1ID)
	createResp := createSessionViaAPI(t, router, body)

	waitForSessionStatus(t, router, createResp.ID, session.StatusActive, 5*time.Second)

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
	require.Equal(t, session.StatusActive, sess.Status)
}

func findSessionByTmuxName(sessions []*session.SessionResponse, tmuxName string) *session.SessionResponse {
	for _, s := range sessions {
		if s.TmuxSession != nil && s.TmuxSession.Name == tmuxName {
			return s
		}
	}
	return nil
}

func TestDaemon_TmuxHookSessionClosed(t *testing.T) {
	_, router, _, ws1ID, _ := setupTestRouter(t)

	body := fmt.Sprintf(`{"name":"test-session","workspace_ids":[%d],"base_branch":"main"}`, ws1ID)
	createResp := createSessionViaAPI(t, router, body)

	waitForSessionStatus(t, router, createResp.ID, session.StatusActive, 5*time.Second)

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
	_, router, mock, ws1ID, _ := setupTestRouter(t)
	mock.SetCreateErr(fmt.Errorf("tmux down"))

	body := fmt.Sprintf(`{"name":"repair-me","workspace_ids":[%d],"base_branch":"main"}`, ws1ID)
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
	require.True(t, mock.HasSessionByName("utena-repair-me"))
}
