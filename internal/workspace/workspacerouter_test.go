package workspace

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupWorkspaceRouter(t *testing.T) (*WorkspaceRouter, *WorkspaceStore) {
	t.Helper()

	database, err := db.OpenInMemory()
	require.NoError(t, err)
	database.Migrate(&Workspace{})
	t.Cleanup(func() { database.Close() })

	store := NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")

	ws1 := &Workspace{Name: "utena", Path: "/path/to/utena", IsGitRepo: true}
	ws2 := &Workspace{Name: "example-project", Path: "/path/to/example", IsGitRepo: false}
	store.Add(ws1)
	store.Add(ws2)

	service := NewWorkspaceService(store)
	gitService := git.NewGitService()
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
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
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
	fs.MkdirAll(configDir, 0755)
	afero.WriteFile(fs, filepath.Join(configDir, "config.json"), []byte(`{}`), 0644)

	database, err := db.OpenInMemory()
	require.NoError(t, err)
	database.Migrate(&Workspace{})
	t.Cleanup(func() { database.Close() })

	store := NewWorkspaceStore(database, fs, configDir)

	wsDir := t.TempDir()

	service := NewWorkspaceService(store)
	gitService := git.NewGitService()
	controller := NewWorkspaceController(service, gitService)
	router := NewWorkspaceRouter(controller)

	body := fmt.Sprintf(`{"path": %q, "as_root": false}`, wsDir)
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)

	var response WorkspaceListResponse
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Len(t, response.Workspaces, 1)
	require.Equal(t, wsDir, response.Workspaces[0].Path)
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "init"},
		{"git", "branch", "develop"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v failed: %s", args, string(out))
	}
	return dir
}

func TestWorkspaceRouter_ListBranches(t *testing.T) {
	repoPath := initTestRepo(t)

	database, err := db.OpenInMemory()
	require.NoError(t, err)
	database.Migrate(&Workspace{})
	t.Cleanup(func() { database.Close() })

	store := NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
	wsGit := &Workspace{Name: "git-repo", Path: repoPath, IsGitRepo: true}
	store.Add(wsGit)

	service := NewWorkspaceService(store)
	gitService := git.NewGitService()
	controller := NewWorkspaceController(service, gitService)
	router := NewWorkspaceRouter(controller)

	req := httptest.NewRequest("GET", fmt.Sprintf("/%d/branches", wsGit.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response BranchListResponse
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Contains(t, response.Branches, "main")
	require.Contains(t, response.Branches, "develop")
	require.Equal(t, "main", response.Branches[0])
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
