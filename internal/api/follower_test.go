package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/mux"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTestFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(contents), mode))
	require.NoError(t, os.Chmod(path, mode))
}

func TestDaemon_SourceFileEndpoint_ServesRawBytes(t *testing.T) {
	app, router, _, ws1ID, _ := setupTestRouter(t)
	ws, err := app.Workspace.Service.GetWorkspace(context.Background(), ws1ID)
	require.NoError(t, err)
	config := `{"setup_actions":[]}`
	writeTestFile(t, filepath.Join(ws.Path, ".utena", "config.json"), config, 0o644)
	writeTestFile(t, filepath.Join(ws.Path, ".env"), "A=1\n", 0o600)
	writeTestFile(t, filepath.Join(ws.Path, ".utena", "worktree-setup"), "#!/bin/sh\n", 0o755)

	cases := []struct {
		path string
		body string
		mode string
	}{
		{".utena/config.json", config, "0644"},
		{".env", "A=1\n", "0600"},
		{".utena/worktree-setup", "#!/bin/sh\n", "0755"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/workspaces/%d/files/%s", ws1ID, tc.path), nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, tc.path)
		assert.Equal(t, tc.body, w.Body.String(), tc.path)
		assert.Equal(t, tc.mode, w.Header().Get(workspace.FileModeHeader), tc.path)
		assert.Empty(t, w.Header().Get("Content-Encoding"), tc.path)
		assert.NotContains(t, w.Header().Get("Content-Type"), "json", tc.path)
	}
}

func TestDaemon_SourceFileEndpoint_MissingAndTraversal(t *testing.T) {
	_, router, _, ws1ID, _ := setupTestRouter(t)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/workspaces/%d/files/.utena/config.json", ws1ID), nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NotEmpty(t, w.Header().Get(workspace.FileMissingHeader))

	for _, p := range []string{"../etc/passwd", "a/../../b.json", "%2e%2e/x"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/workspaces/%d/files/%s", ws1ID, p), nil))
		assert.Equal(t, http.StatusBadRequest, w.Code, p)
	}

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/workspaces/"+workspace.GlobalSetupScriptName, nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NotEmpty(t, w.Header().Get(workspace.FileMissingHeader))
}

type recordingBrowserLeader struct {
	mu       sync.Mutex
	requests []browser.OpenRequest
}

func (l *recordingBrowserLeader) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req browser.OpenRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	l.mu.Lock()
	l.requests = append(l.requests, req)
	l.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"window_id":"w1","tab_id":"t1","focused":true}`))
}

func newFollowerClone(t *testing.T, app *App) string {
	t.Helper()
	clone := initApiTestRepo(t)
	require.NoError(t, app.DB.Service.Create(&git.Repo{Path: clone, FullName: "test/follower"}).Error)
	return clone
}

func setupFollowerRouter(t *testing.T, leaderURL string) (*App, http.Handler) {
	t.Helper()
	gormDB, err := db.OpenInMemorySQLite()
	require.NoError(t, err)

	cfg := Config{ConfigDir: t.TempDir(), SessionsRoot: t.TempDir(), WM: "aerospace", Browser: "chrome-applescript", Follower: true, LeaderURL: leaderURL}
	bus := eventbus.NewEventBus()
	database := db.NewDB(gormDB)
	muxModule := mux.NewModule(mux.NewFakeDriver(), bus, database)
	gitModule := git.NewGitModule(database, bus)
	app := buildApp(gormDB, afero.NewOsFs(), cfg, muxModule, gitModule, bus)
	require.NoError(t, app.OnStart(context.Background()))
	t.Cleanup(func() { app.OnEnd(context.Background()) })
	return app, BuildServer(app, cfg).Handler
}

func doJSON(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestFollower_UsesLeaderForSourcesAndBrowser(t *testing.T) {
	leaderApp, leaderRouter, _, leaderWsID, _ := setupTestRouter(t)
	leaderWs, err := leaderApp.Workspace.Service.GetWorkspace(context.Background(), leaderWsID)
	require.NoError(t, err)
	writeTestFile(t, filepath.Join(leaderWs.Path, ".utena", "config.json"), `{"setup_actions":[{"type":"copy_file","src":".env","dest":".env"}]}`, 0o644)
	writeTestFile(t, filepath.Join(leaderWs.Path, ".env"), "FROM_LEADER=1\n", 0o600)
	writeTestFile(t, filepath.Join(leaderWs.Path, ".utena", "worktree-setup"), "#!/bin/sh\ntouch leader-script-ran\n", 0o755)

	browserLeader := &recordingBrowserLeader{}
	leaderMux := http.NewServeMux()
	leaderMux.Handle("/browser/open", browserLeader)
	leaderMux.Handle("/", leaderRouter)
	leader := httptest.NewServer(leaderMux)
	defer leader.Close()

	followerApp, follower := setupFollowerRouter(t, leader.URL)
	clone := newFollowerClone(t, followerApp)

	w := doJSON(t, follower, http.MethodPost, "/workspaces", fmt.Sprintf(`{"path":%q,"leader_workspace_id":%d}`, clone, leaderWsID))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var added workspace.WorkspaceListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &added))
	require.Len(t, added.Workspaces, 1)
	followerWs := added.Workspaces[0]
	require.NotNil(t, followerWs.LeaderWorkspaceID)
	require.Equal(t, leaderWsID, *followerWs.LeaderWorkspaceID)

	w = doJSON(t, follower, http.MethodPost, "/sessions", fmt.Sprintf(`{"name":"remote","workspaces":[{"workspace_id":%d,"base_branch":"main"}],"leader_session_id":12}`, followerWs.ID))
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var created session.SessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.NotNil(t, created.LeaderSessionID)
	require.Equal(t, uint(12), *created.LeaderSessionID)

	deadline := time.Now().Add(10 * time.Second)
	var got session.SessionResponse
	for time.Now().Before(deadline) {
		w = doJSON(t, follower, http.MethodGet, fmt.Sprintf("/sessions/%d", created.ID), "")
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		if got.Status == session.StatusActive || got.Status == session.StatusBroken {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.Equal(t, session.StatusActive, got.Status, got.StatusError)
	require.Empty(t, got.StatusError)
	require.NotNil(t, got.LeaderSessionID)

	require.Len(t, got.Worktrees, 1)
	require.NotNil(t, got.Worktrees[0].Worktree)
	worktree := got.Worktrees[0].Worktree.Path
	data, err := os.ReadFile(filepath.Join(worktree, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "FROM_LEADER=1\n", string(data))
	_, err = os.Stat(filepath.Join(worktree, "leader-script-ran"))
	assert.NoError(t, err)

	w = doJSON(t, follower, http.MethodPost, "/browser/open", fmt.Sprintf(`{"session_id":%d,"url":"https://example.com/pr"}`, created.ID))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var opened browser.OpenResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &opened))
	assert.Equal(t, "w1", opened.WindowID)
	require.Len(t, browserLeader.requests, 1)
	assert.Equal(t, uint(12), browserLeader.requests[0].SessionID)
	assert.Equal(t, "https://example.com/pr", browserLeader.requests[0].URL)

	w = doJSON(t, follower, http.MethodGet, "/browser/status", "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"connected":false,"outdated":false}`, w.Body.String())

	leader.Close()
	w = doJSON(t, follower, http.MethodPost, "/sessions", fmt.Sprintf(`{"name":"remote-down","workspaces":[{"workspace_id":%d,"base_branch":"main"}]}`, followerWs.ID))
	assert.GreaterOrEqual(t, w.Code, 500, w.Body.String())
	body, _ := io.ReadAll(w.Body)
	assert.Contains(t, string(body), "leader unreachable")
}

func TestFollower_WorkspaceWithoutLeaderIDFailsCreate(t *testing.T) {
	leader := httptest.NewServer(http.NotFoundHandler())
	defer leader.Close()

	followerApp, follower := setupFollowerRouter(t, leader.URL)
	clone := newFollowerClone(t, followerApp)

	w := doJSON(t, follower, http.MethodPost, "/workspaces", fmt.Sprintf(`{"path":%q}`, clone))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var added workspace.WorkspaceListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &added))

	w = doJSON(t, follower, http.MethodPost, "/sessions", fmt.Sprintf(`{"name":"no-leader","workspaces":[{"workspace_id":%d,"base_branch":"main"}]}`, added.Workspaces[0].ID))
	assert.GreaterOrEqual(t, w.Code, 500)
	assert.Contains(t, w.Body.String(), "leader workspace id")
}

func TestDaemon_AddWorkspace_RejectsLeaderIDForRoot(t *testing.T) {
	_, router, _, _, _ := setupTestRouter(t)

	w := doJSON(t, router, http.MethodPost, "/workspaces", fmt.Sprintf(`{"path":%q,"as_root":true,"leader_workspace_id":1}`, t.TempDir()))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBuildApp_NonFollowerUsesBrowserModule(t *testing.T) {
	app, _, _, _, _ := setupTestRouter(t)
	_, ok := app.Browser.(*browser.Module)
	assert.True(t, ok)
}

func TestBuildApp_FollowerUsesFollowerBrowserModule(t *testing.T) {
	app, _ := setupFollowerRouter(t, "http://127.0.0.1:1")
	_, ok := app.Browser.(*browser.FollowerModule)
	assert.True(t, ok)
}

func TestFollower_DoesNotScheduleGitHubJobs(t *testing.T) {
	app, _ := setupFollowerRouter(t, "http://127.0.0.1:1")

	assert.Error(t, app.Jobs.Service.TriggerJob("git.prs"))
	assert.Error(t, app.Jobs.Service.TriggerJob("session.pr_activity"))
	assert.NoError(t, app.Jobs.Service.TriggerJob("git.branches"))
}
