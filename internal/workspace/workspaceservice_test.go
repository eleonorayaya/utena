package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupWorkspaceService(t *testing.T) (*WorkspaceService, *WorkspaceStore) {
	t.Helper()
	database := setupTestDB(t)
	store := NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
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
}

func TestWorkspaceService_OnAppEnd(t *testing.T) {
	service, _ := setupWorkspaceService(t)
	ctx := context.Background()
	err := service.OnAppEnd(ctx)
	require.NoError(t, err)
}

func TestWorkspaceService_ListWorkspaces(t *testing.T) {
	service, store := setupWorkspaceService(t)

	ws1 := &Workspace{Name: "test1", Path: "/path1"}
	ws2 := &Workspace{Name: "test2", Path: "/path2"}
	require.NoError(t, store.Add(ws1))
	require.NoError(t, store.Add(ws2))

	ctx := context.Background()
	workspaces, err := service.ListWorkspaces(ctx)
	require.NoError(t, err)
	require.Len(t, workspaces, 2)

	names := make(map[string]bool)
	for _, ws := range workspaces {
		names[ws.Name] = true
	}
	require.True(t, names["test1"])
	require.True(t, names["test2"])
}

func TestWorkspaceService_GetWorkspace(t *testing.T) {
	service, store := setupWorkspaceService(t)

	ws := &Workspace{
		Name:      "test",
		Path:      "/path/to/test",
		IsGitRepo: true,
	}
	require.NoError(t, store.Add(ws))

	ctx := context.Background()
	retrieved, err := service.GetWorkspace(ctx, ws.ID)
	require.NoError(t, err)
	require.Equal(t, ws.ID, retrieved.ID)
	require.Equal(t, ws.Name, retrieved.Name)
	require.Equal(t, ws.Path, retrieved.Path)
	require.Equal(t, ws.IsGitRepo, retrieved.IsGitRepo)
}

func TestWorkspaceService_GetWorkspace_NotFound(t *testing.T) {
	service, _ := setupWorkspaceService(t)
	ctx := context.Background()
	_, err := service.GetWorkspace(ctx, 99999)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestWorkspaceService_GetWorkspaceByPath(t *testing.T) {
	service, store := setupWorkspaceService(t)

	ws := &Workspace{
		Name: "test",
		Path: "/unique/path",
	}
	require.NoError(t, store.Add(ws))

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
	require.NoError(t, fs.MkdirAll(configDir, 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(configDir, "config.json"), []byte(`{}`), 0644))

	database := testdb.New(t, &Workspace{})

	store := NewWorkspaceStore(database, fs, configDir)
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
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, "project-a"), 0755))

	ctx := context.Background()
	ws, err := service.AddWorkspace(ctx, rootDir, true)
	require.NoError(t, err)
	require.Nil(t, ws)

	workspaces, _ := service.ListWorkspaces(ctx)
	require.Len(t, workspaces, 1)
}

func TestWorkspaceService_SetWorkspaceHidden(t *testing.T) {
	service, store := setupWorkspaceService(t)

	ws := &Workspace{Name: "test", Path: "/path"}
	require.NoError(t, store.Add(ws))

	ctx := context.Background()
	err := service.SetWorkspaceHidden(ctx, ws.ID, true)
	require.NoError(t, err)

	retrieved, err := store.GetByID(ws.ID)
	require.NoError(t, err)
	require.True(t, retrieved.IsHidden)
}

func TestWorkspaceService_SetWorkspaceHidden_NotFound(t *testing.T) {
	service, _ := setupWorkspaceService(t)

	ctx := context.Background()
	err := service.SetWorkspaceHidden(ctx, 99999, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestWorkspaceService_DeleteWorkspace(t *testing.T) {
	service, store := setupWorkspaceService(t)

	ws := &Workspace{Name: "test", Path: "/path"}
	require.NoError(t, store.Add(ws))

	ctx := context.Background()
	err := service.DeleteWorkspace(ctx, ws.ID)
	require.NoError(t, err)

	_, err = store.GetByID(ws.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestWorkspaceService_DeleteWorkspace_NotFound(t *testing.T) {
	service, _ := setupWorkspaceService(t)

	ctx := context.Background()
	err := service.DeleteWorkspace(ctx, 99999)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestWorkspaceService_Touch(t *testing.T) {
	service, store := setupWorkspaceService(t)

	ws := &Workspace{Name: "test", Path: "/path"}
	require.NoError(t, store.Add(ws))

	before := time.Now()
	ctx := context.Background()
	err := service.Touch(ctx, ws.ID)
	require.NoError(t, err)

	retrieved, err := store.GetByID(ws.ID)
	require.NoError(t, err)
	require.False(t, retrieved.LastUsedAt.IsZero())
	require.True(t, retrieved.LastUsedAt.After(before) || retrieved.LastUsedAt.Equal(before))
}

func TestWorkspaceService_Touch_NotFound(t *testing.T) {
	service, _ := setupWorkspaceService(t)

	ctx := context.Background()
	err := service.Touch(ctx, 99999)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}
