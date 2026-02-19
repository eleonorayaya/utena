package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupWorkspaceStore(t *testing.T) *WorkspaceStore {
	t.Helper()
	return NewWorkspaceStore(afero.NewMemMapFs(), "/config")
}

func setupWorkspaceStoreWithConfig(t *testing.T, roots []string) (*WorkspaceStore, string) {
	t.Helper()

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	cfg := config{WorkspaceRoots: roots}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	fs := afero.NewOsFs()
	err = fs.MkdirAll(configDir, 0755)
	require.NoError(t, err)
	err = afero.WriteFile(fs, filepath.Join(configDir, "config.json"), data, 0644)
	require.NoError(t, err)

	store := NewWorkspaceStore(fs, configDir)

	return store, tmpDir
}

func TestNewWorkspaceStore(t *testing.T) {
	store := setupWorkspaceStore(t)
	require.NotNil(t, store)
	require.NotNil(t, store.workspaces)
}

func TestWorkspaceStore_Add(t *testing.T) {
	store := setupWorkspaceStore(t)

	ws := &Workspace{
		ID:        "ws-1",
		Name:      "test",
		Path:      "/path/to/test",
		IsGitRepo: true,
	}

	err := store.Add(ws)
	require.NoError(t, err)

	retrieved, err := store.GetByID("ws-1")
	require.NoError(t, err)
	require.Equal(t, ws.ID, retrieved.ID)
}

func TestWorkspaceStore_Add_NilWorkspace(t *testing.T) {
	store := setupWorkspaceStore(t)
	err := store.Add(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
}

func TestWorkspaceStore_Add_EmptyID(t *testing.T) {
	store := setupWorkspaceStore(t)
	ws := &Workspace{Name: "test", Path: "/path"}
	err := store.Add(ws)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ID cannot be empty")
}

func TestWorkspaceStore_Add_Duplicate(t *testing.T) {
	store := setupWorkspaceStore(t)

	ws1 := &Workspace{ID: "ws-1", Name: "test1", Path: "/path1"}
	ws2 := &Workspace{ID: "ws-1", Name: "test2", Path: "/path2"}

	err := store.Add(ws1)
	require.NoError(t, err)

	err = store.Add(ws2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

func TestWorkspaceStore_GetByID(t *testing.T) {
	store := setupWorkspaceStore(t)

	ws := &Workspace{
		ID:        "ws-1",
		Name:      "test",
		Path:      "/path/to/test",
		IsGitRepo: false,
	}

	store.Add(ws)

	retrieved, err := store.GetByID("ws-1")
	require.NoError(t, err)
	require.Equal(t, ws.ID, retrieved.ID)
	require.Equal(t, ws.Name, retrieved.Name)
	require.Equal(t, ws.Path, retrieved.Path)
	require.Equal(t, ws.IsGitRepo, retrieved.IsGitRepo)
}

func TestWorkspaceStore_GetByID_NotFound(t *testing.T) {
	store := setupWorkspaceStore(t)

	_, err := store.GetByID("nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestWorkspaceStore_GetByPath(t *testing.T) {
	store := setupWorkspaceStore(t)

	ws := &Workspace{
		ID:   "ws-1",
		Name: "test",
		Path: "/unique/path",
	}

	store.Add(ws)

	retrieved, err := store.GetByPath("/unique/path")
	require.NoError(t, err)
	require.Equal(t, ws.ID, retrieved.ID)
}

func TestWorkspaceStore_GetByPath_NotFound(t *testing.T) {
	store := setupWorkspaceStore(t)

	_, err := store.GetByPath("/nonexistent/path")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestWorkspaceStore_List(t *testing.T) {
	store := setupWorkspaceStore(t)

	ws1 := &Workspace{ID: "ws-1", Name: "test1", Path: "/path1"}
	ws2 := &Workspace{ID: "ws-2", Name: "test2", Path: "/path2"}

	store.Add(ws1)
	store.Add(ws2)

	list := store.List()
	require.Len(t, list, 2)

	ids := make(map[string]bool)
	for _, ws := range list {
		ids[ws.ID] = true
	}

	require.True(t, ids["ws-1"], "ws-1 not found in list")
	require.True(t, ids["ws-2"], "ws-2 not found in list")
}

func TestWorkspaceStore_List_SortedAlphabetically(t *testing.T) {
	store := setupWorkspaceStore(t)

	store.Add(&Workspace{ID: "ws-3", Name: "charlie", Path: "/path3"})
	store.Add(&Workspace{ID: "ws-1", Name: "alpha", Path: "/path1"})
	store.Add(&Workspace{ID: "ws-2", Name: "bravo", Path: "/path2"})

	list := store.List()
	require.Len(t, list, 3)
	require.Equal(t, "alpha", list[0].Name)
	require.Equal(t, "bravo", list[1].Name)
	require.Equal(t, "charlie", list[2].Name)
}

func TestWorkspaceStore_List_SortedByLastUsedAt(t *testing.T) {
	store := setupWorkspaceStore(t)

	now := time.Now()
	store.Add(&Workspace{ID: "ws-1", Name: "alpha", Path: "/path1", LastUsedAt: now.Add(-2 * time.Hour)})
	store.Add(&Workspace{ID: "ws-2", Name: "bravo", Path: "/path2", LastUsedAt: now})
	store.Add(&Workspace{ID: "ws-3", Name: "charlie", Path: "/path3"})

	list := store.List()
	require.Len(t, list, 3)
	require.Equal(t, "bravo", list[0].Name, "Most recent should be first")
	require.Equal(t, "alpha", list[1].Name, "Second most recent should be second")
	require.Equal(t, "charlie", list[2].Name, "Never-used should be last")
}

func TestWorkspaceStore_List_Empty(t *testing.T) {
	store := setupWorkspaceStore(t)
	list := store.List()
	require.Empty(t, list)
}

func TestWorkspaceStore_ConcurrentAccess(t *testing.T) {
	store := setupWorkspaceStore(t)

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ws := &Workspace{
				ID:   string(rune('a' + id)),
				Name: "concurrent",
				Path: "/path",
			}
			store.Add(ws)
		}(i)
	}

	wg.Wait()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.List()
		}()
	}

	wg.Wait()

	list := store.List()
	require.Len(t, list, numGoroutines)
}

func TestWorkspaceStore_OnAppStart_WithConfig(t *testing.T) {
	rootDir := t.TempDir()

	os.MkdirAll(filepath.Join(rootDir, "project-alpha", ".git"), 0755)
	os.MkdirAll(filepath.Join(rootDir, "project-beta"), 0755)

	store, _ := setupWorkspaceStoreWithConfig(t, []string{rootDir})

	ctx := context.Background()
	err := store.OnAppStart(ctx)
	require.NoError(t, err)

	workspaces := store.List()
	require.Len(t, workspaces, 2)

	require.Equal(t, "project-alpha", workspaces[0].Name)
	require.Equal(t, filepath.Join(rootDir, "project-alpha"), workspaces[0].Path)
	require.True(t, workspaces[0].IsGitRepo)

	require.Equal(t, "project-beta", workspaces[1].Name)
	require.Equal(t, filepath.Join(rootDir, "project-beta"), workspaces[1].Path)
	require.False(t, workspaces[1].IsGitRepo)
}

func TestWorkspaceStore_OnAppStart_NoConfigFile(t *testing.T) {
	store := NewWorkspaceStore(afero.NewMemMapFs(), "/nonexistent/path")

	ctx := context.Background()
	err := store.OnAppStart(ctx)
	require.NoError(t, err)

	workspaces := store.List()
	require.Empty(t, workspaces)
}

func TestWorkspaceStore_OnAppStart_EmptyRoots(t *testing.T) {
	store, _ := setupWorkspaceStoreWithConfig(t, []string{})

	ctx := context.Background()
	err := store.OnAppStart(ctx)
	require.NoError(t, err)

	workspaces := store.List()
	require.Empty(t, workspaces)
}

func TestWorkspaceStore_OnAppStart_MultipleRoots(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()

	os.MkdirAll(filepath.Join(root1, "project-a", ".git"), 0755)
	os.MkdirAll(filepath.Join(root2, "project-b"), 0755)

	store, _ := setupWorkspaceStoreWithConfig(t, []string{root1, root2})

	ctx := context.Background()
	err := store.OnAppStart(ctx)
	require.NoError(t, err)

	workspaces := store.List()
	require.Len(t, workspaces, 2)

	require.Equal(t, "project-a", workspaces[0].Name)
	require.True(t, workspaces[0].IsGitRepo)

	require.Equal(t, "project-b", workspaces[1].Name)
	require.False(t, workspaces[1].IsGitRepo)
}

func TestWorkspaceStore_OnAppStart_SkipsFiles(t *testing.T) {
	rootDir := t.TempDir()

	os.MkdirAll(filepath.Join(rootDir, "real-project"), 0755)
	os.WriteFile(filepath.Join(rootDir, "not-a-dir.txt"), []byte("hello"), 0644)

	store, _ := setupWorkspaceStoreWithConfig(t, []string{rootDir})

	ctx := context.Background()
	err := store.OnAppStart(ctx)
	require.NoError(t, err)

	workspaces := store.List()
	require.Len(t, workspaces, 1)
	require.Equal(t, "real-project", workspaces[0].Name)
}

func TestWorkspaceStore_OnAppStart_InvalidRootSkipped(t *testing.T) {
	rootDir := t.TempDir()

	os.MkdirAll(filepath.Join(rootDir, "good-project"), 0755)

	store, _ := setupWorkspaceStoreWithConfig(t, []string{"/nonexistent/root", rootDir})

	ctx := context.Background()
	err := store.OnAppStart(ctx)
	require.NoError(t, err)

	workspaces := store.List()
	require.Len(t, workspaces, 1)
	require.Equal(t, "good-project", workspaces[0].Name)
}

func TestWorkspaceStore_OnAppStart_StableIDs(t *testing.T) {
	rootDir := t.TempDir()
	os.MkdirAll(filepath.Join(rootDir, "my-project"), 0755)

	store1, _ := setupWorkspaceStoreWithConfig(t, []string{rootDir})
	ctx := context.Background()
	err := store1.OnAppStart(ctx)
	require.NoError(t, err)

	store2, _ := setupWorkspaceStoreWithConfig(t, []string{rootDir})
	err = store2.OnAppStart(ctx)
	require.NoError(t, err)

	ws1 := store1.List()
	ws2 := store2.List()

	require.Len(t, ws1, 1)
	require.Len(t, ws2, 1)
	require.Equal(t, ws1[0].ID, ws2[0].ID)
}

func TestWorkspaceStore_OnAppEnd(t *testing.T) {
	store := setupWorkspaceStore(t)

	ctx := context.Background()
	err := store.OnAppEnd(ctx)
	require.NoError(t, err)
}

func TestExpandHome(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	result := expandHome("~/dev")
	require.Equal(t, filepath.Join(homeDir, "dev"), result)

	result = expandHome("/absolute/path")
	require.Equal(t, "/absolute/path", result)

	result = expandHome("relative/path")
	require.Equal(t, "relative/path", result)
}

func TestGenerateID(t *testing.T) {
	id1 := generateID("/path/to/project")
	id2 := generateID("/path/to/project")
	id3 := generateID("/different/path")

	require.Equal(t, id1, id2)
	require.NotEqual(t, id1, id3)
	require.Len(t, id1, 16)
}

func TestIsGitRepository(t *testing.T) {
	tmpDir := t.TempDir()

	gitDir := filepath.Join(tmpDir, "git-project")
	os.MkdirAll(filepath.Join(gitDir, ".git"), 0755)

	nonGitDir := filepath.Join(tmpDir, "non-git-project")
	os.MkdirAll(nonGitDir, 0755)

	store := NewWorkspaceStore(afero.NewOsFs(), tmpDir)
	require.True(t, store.isGitRepository(gitDir))
	require.False(t, store.isGitRepository(nonGitDir))
}

func setupWorkspaceStoreWithFullConfig(t *testing.T, roots []string, workspaces []string) (*WorkspaceStore, string) {
	t.Helper()

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	cfg := config{WorkspaceRoots: roots, Workspaces: workspaces}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	fs := afero.NewOsFs()
	err = fs.MkdirAll(configDir, 0755)
	require.NoError(t, err)
	err = afero.WriteFile(fs, filepath.Join(configDir, "config.json"), data, 0644)
	require.NoError(t, err)

	store := NewWorkspaceStore(fs, configDir)

	return store, tmpDir
}

func TestWorkspaceStore_OnAppStart_WithAdHocWorkspaces(t *testing.T) {
	rootDir := t.TempDir()
	adHocDir := t.TempDir()
	os.MkdirAll(filepath.Join(rootDir, "from-root"), 0755)
	os.MkdirAll(filepath.Join(adHocDir, ".git"), 0755)

	store, _ := setupWorkspaceStoreWithFullConfig(t, []string{rootDir}, []string{adHocDir})

	ctx := context.Background()
	err := store.OnAppStart(ctx)
	require.NoError(t, err)

	workspaces := store.List()
	require.Len(t, workspaces, 2)

	paths := make(map[string]bool)
	for _, ws := range workspaces {
		paths[ws.Path] = true
	}
	require.True(t, paths[filepath.Join(rootDir, "from-root")])
	require.True(t, paths[adHocDir])
}

func TestWorkspaceStore_SaveConfig(t *testing.T) {
	store, _ := setupWorkspaceStoreWithFullConfig(t, []string{"~/dev"}, []string{"/some/path"})

	err := store.saveConfig(&config{
		WorkspaceRoots: []string{"~/dev", "~/projects"},
		Workspaces:     []string{"/some/path", "/another/path"},
	})
	require.NoError(t, err)

	cfg, err := store.loadConfig()
	require.NoError(t, err)
	require.Equal(t, []string{"~/dev", "~/projects"}, cfg.WorkspaceRoots)
	require.Equal(t, []string{"/some/path", "/another/path"}, cfg.Workspaces)
}

func TestWorkspaceStore_AddWorkspace(t *testing.T) {
	wsDir := t.TempDir()
	os.MkdirAll(filepath.Join(wsDir, ".git"), 0755)

	store, _ := setupWorkspaceStoreWithFullConfig(t, nil, nil)

	ws, err := store.AddWorkspace(wsDir)
	require.NoError(t, err)
	require.Equal(t, filepath.Base(wsDir), ws.Name)
	require.Equal(t, wsDir, ws.Path)
	require.True(t, ws.IsGitRepo)

	workspaces := store.List()
	require.Len(t, workspaces, 1)

	cfg, err := store.loadConfig()
	require.NoError(t, err)
	require.Contains(t, cfg.Workspaces, wsDir)
}

func TestWorkspaceStore_AddWorkspace_InvalidPath(t *testing.T) {
	store, _ := setupWorkspaceStoreWithFullConfig(t, nil, nil)

	_, err := store.AddWorkspace("/nonexistent/path")
	require.Error(t, err)
}

func TestWorkspaceStore_AddWorkspace_AlreadyExists(t *testing.T) {
	wsDir := t.TempDir()
	store, _ := setupWorkspaceStoreWithFullConfig(t, nil, nil)

	_, err := store.AddWorkspace(wsDir)
	require.NoError(t, err)

	_, err = store.AddWorkspace(wsDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

func TestWorkspaceStore_AddWorkspaceRoot(t *testing.T) {
	rootDir := t.TempDir()
	os.MkdirAll(filepath.Join(rootDir, "proj-a", ".git"), 0755)
	os.MkdirAll(filepath.Join(rootDir, "proj-b"), 0755)

	store, _ := setupWorkspaceStoreWithFullConfig(t, nil, nil)

	added, err := store.AddWorkspaceRoot(rootDir)
	require.NoError(t, err)
	require.Len(t, added, 2)

	workspaces := store.List()
	require.Len(t, workspaces, 2)

	cfg, err := store.loadConfig()
	require.NoError(t, err)
	require.Contains(t, cfg.WorkspaceRoots, rootDir)
}

func TestWorkspaceStore_AddWorkspaceRoot_InvalidPath(t *testing.T) {
	store, _ := setupWorkspaceStoreWithFullConfig(t, nil, nil)

	_, err := store.AddWorkspaceRoot("/nonexistent/path")
	require.Error(t, err)
}

func TestWorkspaceStore_Update(t *testing.T) {
	store := setupWorkspaceStore(t)

	ws := &Workspace{ID: "ws-1", Name: "test", Path: "/path"}
	store.Add(ws)

	now := time.Now()
	ws.LastUsedAt = now
	err := store.Update(ws)
	require.NoError(t, err)

	retrieved, err := store.GetByID("ws-1")
	require.NoError(t, err)
	require.Equal(t, now.Unix(), retrieved.LastUsedAt.Unix())
}

func TestWorkspaceStore_Update_NotFound(t *testing.T) {
	store := setupWorkspaceStore(t)

	ws := &Workspace{ID: "nonexistent", Name: "test", Path: "/path"}
	err := store.Update(ws)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestWorkspaceStore_Update_Nil(t *testing.T) {
	store := setupWorkspaceStore(t)
	err := store.Update(nil)
	require.Error(t, err)
}

func TestWorkspaceStore_Update_EmptyID(t *testing.T) {
	store := setupWorkspaceStore(t)
	err := store.Update(&Workspace{Name: "test"})
	require.Error(t, err)
}

func TestWorkspaceStore_SaveConfig_CreatesDir(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "nested", "dir")

	store := NewWorkspaceStore(afero.NewOsFs(), configDir)

	err := store.saveConfig(&config{Workspaces: []string{"/some/path"}})
	require.NoError(t, err)

	cfg, err := store.loadConfig()
	require.NoError(t, err)
	require.Equal(t, []string{"/some/path"}, cfg.Workspaces)
}
