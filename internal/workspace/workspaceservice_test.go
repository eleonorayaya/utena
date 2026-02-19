package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupWorkspaceService(t *testing.T) (*WorkspaceService, *WorkspaceStore) {
	t.Helper()
	store := NewWorkspaceStore(afero.NewMemMapFs(), "/config")
	service := NewWorkspaceService(store)
	return service, store
}

func TestNewWorkspaceService(t *testing.T) {
	service, _ := setupWorkspaceService(t)
	require.NotNil(t, service)
	require.NotNil(t, service.store)
}

func TestWorkspaceService_OnAppStart(t *testing.T) {
	service, _ := setupWorkspaceService(t)
	ctx := context.Background()
	err := service.OnAppStart(ctx)
	require.NoError(t, err)

	// Service OnAppStart is a no-op
	// Store handles initialization
}

func TestWorkspaceService_OnAppEnd(t *testing.T) {
	service, _ := setupWorkspaceService(t)
	ctx := context.Background()
	err := service.OnAppEnd(ctx)
	require.NoError(t, err)
}

func TestWorkspaceService_ListWorkspaces(t *testing.T) {
	service, store := setupWorkspaceService(t)

	// Add test workspaces
	ws1 := &Workspace{ID: "ws-1", Name: "test1", Path: "/path1"}
	ws2 := &Workspace{ID: "ws-2", Name: "test2", Path: "/path2"}
	store.Add(ws1)
	store.Add(ws2)

	ctx := context.Background()
	workspaces, err := service.ListWorkspaces(ctx)
	require.NoError(t, err)
	require.Len(t, workspaces, 2)

	// Verify both workspaces are in the list
	ids := make(map[string]bool)
	for _, ws := range workspaces {
		ids[ws.ID] = true
	}
	require.True(t, ids["ws-1"])
	require.True(t, ids["ws-2"])
}

func TestWorkspaceService_GetWorkspace(t *testing.T) {
	service, store := setupWorkspaceService(t)

	ws := &Workspace{
		ID:        "ws-1",
		Name:      "test",
		Path:      "/path/to/test",
		IsGitRepo: true,
	}
	store.Add(ws)

	ctx := context.Background()
	retrieved, err := service.GetWorkspace(ctx, "ws-1")
	require.NoError(t, err)
	require.Equal(t, ws.ID, retrieved.ID)
	require.Equal(t, ws.Name, retrieved.Name)
	require.Equal(t, ws.Path, retrieved.Path)
	require.Equal(t, ws.IsGitRepo, retrieved.IsGitRepo)
}

func TestWorkspaceService_GetWorkspace_NotFound(t *testing.T) {
	service, _ := setupWorkspaceService(t)
	ctx := context.Background()
	_, err := service.GetWorkspace(ctx, "nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestWorkspaceService_GetWorkspaceByPath(t *testing.T) {
	service, store := setupWorkspaceService(t)

	ws := &Workspace{
		ID:   "ws-1",
		Name: "test",
		Path: "/unique/path",
	}
	store.Add(ws)

	ctx := context.Background()
	retrieved, err := service.GetWorkspaceByPath(ctx, "/unique/path")
	require.NoError(t, err)
	require.Equal(t, ws.ID, retrieved.ID)
	require.Equal(t, ws.Path, retrieved.Path)
}

func TestWorkspaceService_GetWorkspaceByPath_NotFound(t *testing.T) {
	service, _ := setupWorkspaceService(t)
	ctx := context.Background()
	_, err := service.GetWorkspaceByPath(ctx, "/nonexistent/path")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func setupWorkspaceServiceWithConfig(t *testing.T) (*WorkspaceService, *WorkspaceStore) {
	t.Helper()
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	fs := afero.NewOsFs()
	fs.MkdirAll(configDir, 0755)
	afero.WriteFile(fs, filepath.Join(configDir, "config.json"), []byte(`{}`), 0644)

	store := NewWorkspaceStore(fs, configDir)
	service := NewWorkspaceService(store)
	return service, store
}

func TestWorkspaceService_AddWorkspace(t *testing.T) {
	service, _ := setupWorkspaceServiceWithConfig(t)
	wsDir := t.TempDir()

	ctx := context.Background()
	ws, err := service.AddWorkspace(ctx, wsDir, false)
	require.NoError(t, err)
	require.Equal(t, filepath.Base(wsDir), ws.Name)
	require.Equal(t, wsDir, ws.Path)
}

func TestWorkspaceService_AddWorkspaceAsRoot(t *testing.T) {
	service, _ := setupWorkspaceServiceWithConfig(t)
	rootDir := t.TempDir()
	os.MkdirAll(filepath.Join(rootDir, "project-a"), 0755)

	ctx := context.Background()
	ws, err := service.AddWorkspace(ctx, rootDir, true)
	require.NoError(t, err)
	require.Nil(t, ws)

	workspaces, _ := service.ListWorkspaces(ctx)
	require.Len(t, workspaces, 1)
}
