package workspace

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func setupWorkspaceRouter(t *testing.T) (*WorkspaceRouter, *WorkspaceStore) {
	t.Helper()

	store := NewWorkspaceStore()

	store.Add(&Workspace{ID: "ws-1", Name: "utena", Path: "/path/to/utena", IsGitRepo: true})
	store.Add(&Workspace{ID: "ws-2", Name: "example-project", Path: "/path/to/example", IsGitRepo: false})

	service := NewWorkspaceService(store)
	controller := NewWorkspaceController(service)
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

	ids := make(map[string]bool)
	for _, ws := range response.Workspaces {
		ids[ws.ID] = true
	}
	require.True(t, ids["ws-1"])
	require.True(t, ids["ws-2"])
}

func TestWorkspaceRouter_GetWorkspaceByID(t *testing.T) {
	router, _ := setupWorkspaceRouter(t)

	req := httptest.NewRequest("GET", "/ws-1", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response WorkspaceResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "ws-1", response.ID)
	require.Equal(t, "utena", response.Name)
	require.Equal(t, "/path/to/utena", response.Path)
	require.True(t, response.IsGitRepo)
}

func TestWorkspaceRouter_GetWorkspaceByID_NotFound(t *testing.T) {
	router, _ := setupWorkspaceRouter(t)

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestWorkspaceRouter_AddWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	os.WriteFile(configPath, []byte(`{}`), 0644)

	store := NewWorkspaceStore()
	store.configPath = configPath

	wsDir := t.TempDir()

	service := NewWorkspaceService(store)
	controller := NewWorkspaceController(service)
	router := NewWorkspaceRouter(controller)

	body := fmt.Sprintf(`{"path": %q, "as_root": false}`, wsDir)
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)

	var response WorkspaceListResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Len(t, response.Workspaces, 1)
	require.Equal(t, wsDir, response.Workspaces[0].Path)
}
