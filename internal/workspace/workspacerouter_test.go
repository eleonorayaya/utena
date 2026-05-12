package workspace

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupWorkspaceRouter(t *testing.T) (*WorkspaceRouter, *WorkspaceStore) {
	t.Helper()

	database := testdb.New(t, &Workspace{}, &git.Repo{}, &git.Branch{}, &git.Worktree{}, &git.PullRequest{})

	store := NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")

	ws1 := &Workspace{Name: "utena", Path: "/path/to/utena", IsGitRepo: true}
	ws2 := &Workspace{Name: "example-project", Path: "/path/to/example", IsGitRepo: false}
	require.NoError(t, store.Add(ws1))
	require.NoError(t, store.Add(ws2))

	gitService := git.NewGitService(database)
	service := NewWorkspaceService(store, gitService)
	controller := NewWorkspaceController(service, gitService)
	router := NewWorkspaceRouter(controller)

	return router, store
}

func TestWorkspaceRouter_ListWorkspaces(t *testing.T) {
	router, _ := setupWorkspaceRouter(t)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response WorkspaceListResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Len(t, response.Workspaces, 2)

	names := make(map[string]bool)
	for _, ws := range response.Workspaces {
		require.NotZero(t, ws.ID)
		names[ws.Name] = true
	}
	require.True(t, names["utena"])
	require.True(t, names["example-project"])
}

func TestWorkspaceRouter_GetWorkspaceByID(t *testing.T) {
	router, store := setupWorkspaceRouter(t)

	ws, err := store.GetByPath("/path/to/utena")
	require.NoError(t, err)

	req := httptest.NewRequest("GET", fmt.Sprintf("/%d", ws.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response WorkspaceResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, ws.ID, response.ID)
	require.Equal(t, "utena", response.Name)
	require.Equal(t, "/path/to/utena", response.Path)
	require.True(t, response.IsGitRepo)
}

func TestWorkspaceRouter_GetWorkspaceByID_NotFound(t *testing.T) {
	router, _ := setupWorkspaceRouter(t)

	req := httptest.NewRequest("GET", "/99999", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestWorkspaceRouter_AddWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fs := afero.NewOsFs()
	require.NoError(t, fs.MkdirAll(configDir, 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(configDir, "config.json"), []byte(`{}`), 0644))

	database := testdb.New(t, &Workspace{}, &git.Repo{}, &git.Branch{}, &git.Worktree{}, &git.PullRequest{})

	store := NewWorkspaceStore(database, fs, configDir)

	wsDir := initTestRepo(t)

	gitService := git.NewGitService(database)
	service := NewWorkspaceService(store, gitService)
	controller := NewWorkspaceController(service, gitService)
	router := NewWorkspaceRouter(controller)

	body := fmt.Sprintf(`{"path": %q, "as_root": false}`, wsDir)
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)

	var response WorkspaceListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Len(t, response.Workspaces, 1)
	require.Equal(t, wsDir, response.Workspaces[0].Path)
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitEnv := append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "config", "commit.gpgsign", "false"},
		{"git", "config", "tag.gpgsign", "false"},
		{"git", "commit", "--allow-empty", "-m", "init"},
		{"git", "branch", "develop"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = gitEnv
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v failed: %s", args, string(out))
	}
	return dir
}

func TestWorkspaceRouter_ListBranches(t *testing.T) {
	repoPath := initTestRepo(t)

	database := testdb.New(t, &Workspace{}, &git.Repo{}, &git.Branch{}, &git.Worktree{}, &git.PullRequest{})

	store := NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
	wsGit := &Workspace{Name: "git-repo", Path: repoPath, IsGitRepo: true}
	require.NoError(t, store.Add(wsGit))

	gitService := git.NewGitService(database)
	service := NewWorkspaceService(store, gitService)
	controller := NewWorkspaceController(service, gitService)
	router := NewWorkspaceRouter(controller)

	req := httptest.NewRequest("GET", fmt.Sprintf("/%d/branches", wsGit.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response BranchListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Contains(t, response.Branches, "main")
	require.Contains(t, response.Branches, "develop")
	require.Equal(t, "main", response.Branches[0])
}

func TestWorkspaceRouter_SetWorkspaceHidden(t *testing.T) {
	router, store := setupWorkspaceRouter(t)

	ws, err := store.GetByPath("/path/to/utena")
	require.NoError(t, err)
	require.False(t, ws.IsHidden)

	body := `{"hidden": true}`
	req := httptest.NewRequest("PUT", fmt.Sprintf("/%d/hidden", ws.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)

	updated, err := store.GetByID(ws.ID)
	require.NoError(t, err)
	require.True(t, updated.IsHidden)
}

func TestWorkspaceRouter_SetWorkspaceHidden_NotFound(t *testing.T) {
	router, _ := setupWorkspaceRouter(t)

	body := `{"hidden": true}`
	req := httptest.NewRequest("PUT", "/99999/hidden", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestWorkspaceRouter_DeleteWorkspace(t *testing.T) {
	router, store := setupWorkspaceRouter(t)

	ws, err := store.GetByPath("/path/to/utena")
	require.NoError(t, err)

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/%d", ws.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)

	_, err = store.GetByID(ws.ID)
	require.Error(t, err)
}

func TestWorkspaceRouter_DeleteWorkspace_NotFound(t *testing.T) {
	router, _ := setupWorkspaceRouter(t)

	req := httptest.NewRequest("DELETE", "/99999", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestWorkspaceRouter_ListBranches_NotGitRepo(t *testing.T) {
	router, store := setupWorkspaceRouter(t)

	ws, err := store.GetByPath("/path/to/example")
	require.NoError(t, err)

	req := httptest.NewRequest("GET", fmt.Sprintf("/%d/branches", ws.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestWorkspaceRouter_ListBranches_NotFound(t *testing.T) {
	router, _ := setupWorkspaceRouter(t)

	req := httptest.NewRequest("GET", "/99999/branches", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func initTestRepoWithOrigin(t *testing.T) string {
	t.Helper()
	origin := t.TempDir()
	runCmd(t, origin, "git", "init", "--bare", "-b", "main")

	upstream := t.TempDir()
	for _, args := range [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "config", "commit.gpgsign", "false"},
		{"git", "config", "tag.gpgsign", "false"},
		{"git", "commit", "--allow-empty", "-m", "init"},
		{"git", "remote", "add", "origin", origin},
		{"git", "push", "origin", "main"},
		{"git", "branch", "remote-branch"},
		{"git", "push", "origin", "remote-branch"},
	} {
		runCmd(t, upstream, args[0], args[1:]...)
	}

	clone := t.TempDir()
	runCmd(t, clone, "git", "clone", origin, ".")
	runCmd(t, clone, "git", "config", "user.email", "test@test.com")
	runCmd(t, clone, "git", "config", "user.name", "Test")
	runCmd(t, clone, "git", "config", "commit.gpgsign", "false")
	runCmd(t, clone, "git", "config", "tag.gpgsign", "false")
	runCmd(t, clone, "git", "branch", "local-only")
	return clone
}

func runCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "command %s %v failed: %s", name, args, string(out))
}

func setupBranchRouter(t *testing.T, repoPath string) (*WorkspaceRouter, *Workspace) {
	t.Helper()

	database := testdb.New(t, &Workspace{}, &git.Repo{}, &git.Branch{}, &git.Worktree{}, &git.PullRequest{})

	store := NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
	ws := &Workspace{Name: "git-repo", Path: repoPath, IsGitRepo: true}
	require.NoError(t, store.Add(ws))
	notGit := &Workspace{Name: "plain", Path: "/plain", IsGitRepo: false}
	require.NoError(t, store.Add(notGit))

	gitService := git.NewGitService(database)
	service := NewWorkspaceService(store, gitService)
	controller := NewWorkspaceController(service, gitService)
	router := NewWorkspaceRouter(controller)

	return router, ws
}

func TestWorkspaceRouter_FetchAndListBranches(t *testing.T) {
	repoPath := initTestRepoWithOrigin(t)
	router, ws := setupBranchRouter(t, repoPath)

	req := httptest.NewRequest("POST", fmt.Sprintf("/%d/branches/fetch", ws.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response BranchRefListResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	byName := map[string]git.BranchRef{}
	for _, r := range response.Branches {
		byName[r.Name] = r
	}
	require.Contains(t, byName, "main")
	require.False(t, byName["main"].Remote)
	require.Contains(t, byName, "local-only")
	require.False(t, byName["local-only"].Remote)
	require.Contains(t, byName, "remote-branch")
	require.True(t, byName["remote-branch"].Remote)
	require.Equal(t, "main", response.Branches[0].Name)
}

func TestWorkspaceRouter_FetchAndListBranches_NotGitRepo(t *testing.T) {
	repoPath := initTestRepoWithOrigin(t)
	router, _ := setupBranchRouter(t, repoPath)

	req := httptest.NewRequest("POST", "/2/branches/fetch", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestWorkspaceRouter_CheckBranchExists_Remote(t *testing.T) {
	repoPath := initTestRepoWithOrigin(t)
	router, ws := setupBranchRouter(t, repoPath)

	req := httptest.NewRequest("GET", fmt.Sprintf("/%d/branches/exists?name=remote-branch", ws.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response BranchExistsResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.False(t, response.ExistsLocal)
	require.True(t, response.ExistsRemote)
}

func TestWorkspaceRouter_CheckBranchExists_Local(t *testing.T) {
	repoPath := initTestRepoWithOrigin(t)
	router, ws := setupBranchRouter(t, repoPath)

	req := httptest.NewRequest("GET", fmt.Sprintf("/%d/branches/exists?name=local-only", ws.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response BranchExistsResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.True(t, response.ExistsLocal)
	require.False(t, response.ExistsRemote)
}

func TestWorkspaceRouter_CheckBranchExists_NotFound(t *testing.T) {
	repoPath := initTestRepoWithOrigin(t)
	router, ws := setupBranchRouter(t, repoPath)

	req := httptest.NewRequest("GET", fmt.Sprintf("/%d/branches/exists?name=does-not-exist", ws.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response BranchExistsResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.False(t, response.ExistsLocal)
	require.False(t, response.ExistsRemote)
}

func TestWorkspaceRouter_CheckBranchExists_MissingName(t *testing.T) {
	repoPath := initTestRepoWithOrigin(t)
	router, ws := setupBranchRouter(t, repoPath)

	req := httptest.NewRequest("GET", fmt.Sprintf("/%d/branches/exists", ws.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestWorkspaceRouter_MigrateToBare_BootstrapsClaudeSettings(t *testing.T) {
	repoPath := initTestRepoWithOrigin(t)
	router, ws := setupBranchRouter(t, repoPath)

	req := httptest.NewRequest("POST", fmt.Sprintf("/%d/migrate-bare", ws.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)

	settingsPath := filepath.Join(repoPath, ".claude", "settings.local.json")
	data, err := os.ReadFile(settingsPath)
	require.NoError(t, err, "settings.local.json should exist after bare migration")
	require.NotEmpty(t, data)
}
