package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupWorkspaceService(t *testing.T) (*WorkspaceService, *WorkspaceStore) {
	t.Helper()
	database := setupTestDB(t)
	store := NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
	service := NewWorkspaceService(store, nil)
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
	service := NewWorkspaceService(store, nil)
	return service, store
}

func TestWorkspaceService_AddWorkspace_GitRepo(t *testing.T) {
	service, _ := setupWorkspaceServiceWithConfig(t)
	wsDir := initTestRepo(t)

	ctx := context.Background()
	ws, err := service.AddWorkspace(ctx, wsDir, false)
	require.NoError(t, err)
	require.NotNil(t, ws)
	require.Equal(t, filepath.Base(wsDir), ws.Name)
	require.Equal(t, wsDir, ws.Path)
	require.True(t, ws.IsGitRepo)
}

func TestWorkspaceService_AddWorkspace_RejectsNonGit(t *testing.T) {
	service, _ := setupWorkspaceServiceWithConfig(t)
	wsDir := t.TempDir()

	ctx := context.Background()
	ws, err := service.AddWorkspace(ctx, wsDir, false)
	require.Error(t, err)
	require.Nil(t, ws)
	require.Contains(t, err.Error(), "not a git repository")

	workspaces, _ := service.ListWorkspaces(ctx)
	require.Empty(t, workspaces, "rejected workspace should not be persisted")
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

func setupServiceWithGitAndRoot(t *testing.T) (*WorkspaceService, string) {
	t.Helper()
	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "projects")
	require.NoError(t, os.MkdirAll(rootDir, 0755))

	configDir := filepath.Join(tmpDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0755))
	cfgBytes, err := json.Marshal(config{WorkspaceRoots: []string{rootDir}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), cfgBytes, 0644))

	database := testdb.New(t, &Workspace{}, &git.Repo{})
	store := NewWorkspaceStore(database, afero.NewOsFs(), configDir)
	gitService := git.NewGitService(database)
	service := NewWorkspaceService(store, gitService)
	return service, rootDir
}

func setupServiceWithGitAndRoots(t *testing.T, rootCount int) (*WorkspaceService, []string) {
	t.Helper()
	tmpDir := t.TempDir()

	roots := make([]string, 0, rootCount)
	for i := range rootCount {
		r := filepath.Join(tmpDir, "root", string(rune('a'+i)))
		require.NoError(t, os.MkdirAll(r, 0755))
		roots = append(roots, r)
	}

	configDir := filepath.Join(tmpDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0755))
	cfgBytes, err := json.Marshal(config{WorkspaceRoots: roots})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), cfgBytes, 0644))

	database := testdb.New(t, &Workspace{}, &git.Repo{})
	store := NewWorkspaceStore(database, afero.NewOsFs(), configDir)
	gitService := git.NewGitService(database)
	service := NewWorkspaceService(store, gitService)
	return service, roots
}

func TestWorkspaceService_CloneAndAddFromURL_HappyPath(t *testing.T) {
	service, rootDir := setupServiceWithGitAndRoot(t)
	source := initTestRepo(t)

	ws, err := service.CloneAndAddFromURL(context.Background(), CloneFromURLRequest{
		CloneURL: source,
		DirName:  "imported",
	})
	require.NoError(t, err)
	require.NotNil(t, ws)
	require.Equal(t, filepath.Join(rootDir, "imported"), ws.Path)
	require.True(t, ws.IsBare)
	require.True(t, ws.IsGitRepo)

	bareInfo, err := os.Stat(filepath.Join(ws.Path, ".bare"))
	require.NoError(t, err)
	require.True(t, bareInfo.IsDir())
}

func TestWorkspaceService_CloneAndAddFromURL_DerivesDirNameFromURL(t *testing.T) {
	service, rootDir := setupServiceWithGitAndRoot(t)

	_, err := service.CloneAndAddFromURL(context.Background(), CloneFromURLRequest{
		CloneURL: "git@github.com:foo/bar.git",
	})
	require.Error(t, err, "expected clone to fail with unreachable remote so we can verify path derivation")

	_, statErr := os.Stat(filepath.Join(rootDir, "bar"))
	require.True(t, os.IsNotExist(statErr), "failed clone should clean up the derived target dir")
}

func TestWorkspaceService_CloneAndAddFromURL_RejectsEmptyURL(t *testing.T) {
	service, _ := setupServiceWithGitAndRoot(t)
	_, err := service.CloneAndAddFromURL(context.Background(), CloneFromURLRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "clone_url is required")
}

func TestWorkspaceService_CloneAndAddFromURL_RejectsTargetExists(t *testing.T) {
	service, rootDir := setupServiceWithGitAndRoot(t)
	existing := filepath.Join(rootDir, "imported")
	require.NoError(t, os.MkdirAll(existing, 0755))

	source := initTestRepo(t)
	_, err := service.CloneAndAddFromURL(context.Background(), CloneFromURLRequest{
		CloneURL: source,
		DirName:  "imported",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

func TestWorkspaceService_CloneAndAddFromURL_NoConfiguredRoots(t *testing.T) {
	service, _ := setupServiceWithGitAndRoots(t, 0)
	source := initTestRepo(t)

	_, err := service.CloneAndAddFromURL(context.Background(), CloneFromURLRequest{CloneURL: source, DirName: "x"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no workspace roots configured")
}

func TestWorkspaceService_CloneAndAddFromURL_MultipleRootsRequiresExplicit(t *testing.T) {
	service, roots := setupServiceWithGitAndRoots(t, 2)
	source := initTestRepo(t)

	_, err := service.CloneAndAddFromURL(context.Background(), CloneFromURLRequest{CloneURL: source, DirName: "x"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "multiple workspace roots")

	ws, err := service.CloneAndAddFromURL(context.Background(), CloneFromURLRequest{
		CloneURL: source,
		RootPath: roots[1],
		DirName:  "x",
	})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(roots[1], "x"), ws.Path)
}

func TestWorkspaceService_CloneAndAddFromURL_RejectsUnknownRoot(t *testing.T) {
	service, _ := setupServiceWithGitAndRoot(t)
	source := initTestRepo(t)

	_, err := service.CloneAndAddFromURL(context.Background(), CloneFromURLRequest{
		CloneURL: source,
		RootPath: "/nope",
		DirName:  "x",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a configured workspace root")
}

func TestWorkspaceService_CloneAndAddFromURL_RejectsPathSeparatorInDirName(t *testing.T) {
	service, _ := setupServiceWithGitAndRoot(t)
	source := initTestRepo(t)

	_, err := service.CloneAndAddFromURL(context.Background(), CloneFromURLRequest{
		CloneURL: source,
		DirName:  "weird/name",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid directory name")
}
