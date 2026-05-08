package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) db.Database {
	return testdb.New(t, &git.Repo{}, &Workspace{})
}

func setupWorkspaceStore(t *testing.T) *WorkspaceStore {
	t.Helper()
	database := setupTestDB(t)
	return NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
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

	database := setupTestDB(t)
	store := NewWorkspaceStore(database, fs, configDir)

	return store, tmpDir
}

func TestNewWorkspaceStore(t *testing.T) {
	store := setupWorkspaceStore(t)
	require.NotNil(t, store)
	require.Empty(t, store.List())
}

func TestWorkspaceStore_Add(t *testing.T) {
	store := setupWorkspaceStore(t)

	ws := &Workspace{
		Name:      "test",
		Path:      "/path/to/test",
		IsGitRepo: true,
	}

	err := store.Add(ws)
	require.NoError(t, err)
	require.NotZero(t, ws.ID)

	retrieved, err := store.GetByID(ws.ID)
	require.NoError(t, err)
	require.Equal(t, ws.ID, retrieved.ID)
}

func TestWorkspaceStore_Add_NilWorkspace(t *testing.T) {
	store := setupWorkspaceStore(t)
	err := store.Add(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
}

func TestWorkspaceStore_Add_Duplicate(t *testing.T) {
	store := setupWorkspaceStore(t)

	ws1 := &Workspace{Name: "test1", Path: "/same/path"}
	ws2 := &Workspace{Name: "test2", Path: "/same/path"}

	err := store.Add(ws1)
	require.NoError(t, err)

	err = store.Add(ws2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")

	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, common.CategoryConflict, appErr.Category)
	require.Contains(t, err.Error(), "/same/path")
}

func TestWorkspaceStore_Add_DuplicateRepo(t *testing.T) {
	store := setupWorkspaceStore(t)
	repo := &git.Repo{Path: "/test/shared", FullName: "owner/shared"}
	require.NoError(t, store.db.Create(repo).Error)

	require.NoError(t, store.Add(&Workspace{Name: "first", Path: "/path/a", RepoID: &repo.ID}))

	err := store.Add(&Workspace{Name: "second", Path: "/path/b", RepoID: &repo.ID})
	require.Error(t, err)

	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, common.CategoryConflict, appErr.Category)
	require.Contains(t, err.Error(), "repo")
}

func TestWorkspaceStore_GetByID(t *testing.T) {
	store := setupWorkspaceStore(t)

	ws := &Workspace{
		Name:      "test",
		Path:      "/path/to/test",
		IsGitRepo: false,
	}

	require.NoError(t, store.Add(ws))

	retrieved, err := store.GetByID(ws.ID)
	require.NoError(t, err)
	require.Equal(t, ws.ID, retrieved.ID)
	require.Equal(t, ws.Name, retrieved.Name)
	require.Equal(t, ws.Path, retrieved.Path)
	require.Equal(t, ws.IsGitRepo, retrieved.IsGitRepo)
}

func TestWorkspaceStore_GetByID_NotFound(t *testing.T) {
	store := setupWorkspaceStore(t)

	_, err := store.GetByID(99999)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestWorkspaceStore_GetByPath(t *testing.T) {
	store := setupWorkspaceStore(t)

	ws := &Workspace{
		Name: "test",
		Path: "/unique/path",
	}

	require.NoError(t, store.Add(ws))

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

	ws1 := &Workspace{Name: "test1", Path: "/path1"}
	ws2 := &Workspace{Name: "test2", Path: "/path2"}

	require.NoError(t, store.Add(ws1))
	require.NoError(t, store.Add(ws2))

	list := store.List()
	require.Len(t, list, 2)

	names := make(map[string]bool)
	for _, ws := range list {
		require.NotZero(t, ws.ID)
		names[ws.Name] = true
	}

	require.True(t, names["test1"], "test1 not found in list")
	require.True(t, names["test2"], "test2 not found in list")
}

func TestWorkspaceStore_List_SortedAlphabetically(t *testing.T) {
	store := setupWorkspaceStore(t)

	require.NoError(t, store.Add(&Workspace{Name: "charlie", Path: "/path3"}))
	require.NoError(t, store.Add(&Workspace{Name: "alpha", Path: "/path1"}))
	require.NoError(t, store.Add(&Workspace{Name: "bravo", Path: "/path2"}))

	list := store.List()
	require.Len(t, list, 3)
	require.Equal(t, "alpha", list[0].Name)
	require.Equal(t, "bravo", list[1].Name)
	require.Equal(t, "charlie", list[2].Name)
}

func TestWorkspaceStore_List_SortedByLastUsedAt(t *testing.T) {
	store := setupWorkspaceStore(t)

	now := time.Now()
	require.NoError(t, store.Add(&Workspace{Name: "alpha", Path: "/path1", LastUsedAt: now.Add(-2 * time.Hour)}))
	require.NoError(t, store.Add(&Workspace{Name: "bravo", Path: "/path2", LastUsedAt: now}))
	require.NoError(t, store.Add(&Workspace{Name: "charlie", Path: "/path3"}))

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
				Name: fmt.Sprintf("concurrent-%d", id),
				Path: fmt.Sprintf("/path/%d", id),
			}
			require.NoError(t, store.Add(ws))
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

	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, "project-alpha", ".git"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, "project-beta"), 0755))

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
	database := setupTestDB(t)
	store := NewWorkspaceStore(database, afero.NewMemMapFs(), "/nonexistent/path")

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

	require.NoError(t, os.MkdirAll(filepath.Join(root1, "project-a", ".git"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root2, "project-b"), 0755))

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

	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, "real-project"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(rootDir, "not-a-dir.txt"), []byte("hello"), 0644))

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

	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, "good-project"), 0755))

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
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, "my-project"), 0755))

	store1, _ := setupWorkspaceStoreWithConfig(t, []string{rootDir})
	ctx := context.Background()
	err := store1.OnAppStart(ctx)
	require.NoError(t, err)

	ws1 := store1.List()
	require.Len(t, ws1, 1)
	require.Equal(t, "my-project", ws1[0].Name)

	err = store1.OnAppStart(ctx)
	require.NoError(t, err)

	ws2 := store1.List()
	require.Len(t, ws2, 1)
	require.Equal(t, ws1[0].Path, ws2[0].Path)
	require.Equal(t, "my-project", ws2[0].Name)
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

func TestIsGitRepository(t *testing.T) {
	tmpDir := t.TempDir()

	gitDir := filepath.Join(tmpDir, "git-project")
	require.NoError(t, os.MkdirAll(filepath.Join(gitDir, ".git"), 0755))

	nonGitDir := filepath.Join(tmpDir, "non-git-project")
	require.NoError(t, os.MkdirAll(nonGitDir, 0755))

	database := setupTestDB(t)
	store := NewWorkspaceStore(database, afero.NewOsFs(), tmpDir)
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

	database := setupTestDB(t)
	store := NewWorkspaceStore(database, fs, configDir)

	return store, tmpDir
}

func TestWorkspaceStore_OnAppStart_WithAdHocWorkspaces(t *testing.T) {
	rootDir := t.TempDir()
	adHocDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, "from-root"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(adHocDir, ".git"), 0755))

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
	require.NoError(t, os.MkdirAll(filepath.Join(wsDir, ".git"), 0755))

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
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, "proj-a", ".git"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, "proj-b"), 0755))

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

	ws := &Workspace{Name: "test", Path: "/path"}
	require.NoError(t, store.Add(ws))

	now := time.Now()
	ws.LastUsedAt = now
	err := store.Update(ws)
	require.NoError(t, err)

	retrieved, err := store.GetByID(ws.ID)
	require.NoError(t, err)
	require.Equal(t, now.Unix(), retrieved.LastUsedAt.Unix())
}

func TestWorkspaceStore_Update_NotFound(t *testing.T) {
	store := setupWorkspaceStore(t)

	ws := &Workspace{Name: "test", Path: "/path"}
	ws.ID = 99999
	err := store.Update(ws)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestWorkspaceStore_Update_Nil(t *testing.T) {
	store := setupWorkspaceStore(t)
	err := store.Update(nil)
	require.Error(t, err)
}

func TestWorkspaceStore_Update_ZeroID(t *testing.T) {
	store := setupWorkspaceStore(t)
	err := store.Update(&Workspace{Name: "test"})
	require.Error(t, err)
}

func TestWorkspaceStore_SaveConfig_CreatesDir(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "nested", "dir")

	database := setupTestDB(t)
	store := NewWorkspaceStore(database, afero.NewOsFs(), configDir)

	err := store.saveConfig(&config{Workspaces: []string{"/some/path"}})
	require.NoError(t, err)

	cfg, err := store.loadConfig()
	require.NoError(t, err)
	require.Equal(t, []string{"/some/path"}, cfg.Workspaces)
}

func TestWorkspaceStore_Persistence(t *testing.T) {
	database := setupTestDB(t)
	store := NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")

	now := time.Now()
	ws := &Workspace{Name: "test", Path: "/path", LastUsedAt: now}
	require.NoError(t, store.Add(ws))

	store2 := NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")

	retrieved, err := store2.GetByID(ws.ID)
	require.NoError(t, err)
	require.Equal(t, now.Unix(), retrieved.LastUsedAt.Unix())
	require.Equal(t, "test", retrieved.Name)
}

func TestWorkspaceStore_GetByRepoID(t *testing.T) {
	store := setupWorkspaceStore(t)
	repo := &git.Repo{Path: "/test/my-ws", FullName: "owner/my-ws"}
	require.NoError(t, store.db.Create(repo).Error)

	ws := &Workspace{Name: "my-ws", Path: "/test/my-ws", IsGitRepo: true, RepoID: &repo.ID}
	require.NoError(t, store.Add(ws))

	found, err := store.GetByRepoID(repo.ID)
	require.NoError(t, err)
	require.Equal(t, ws.ID, found.ID)
	require.Equal(t, repo.ID, *found.RepoID)
}

func TestWorkspaceStore_GetByRepoID_NotFound(t *testing.T) {
	store := setupWorkspaceStore(t)
	_, err := store.GetByRepoID(999)
	require.Error(t, err)
}

func TestWorkspaceStore_Delete(t *testing.T) {
	store := setupWorkspaceStore(t)

	ws := &Workspace{Name: "test", Path: "/path/to/test"}
	require.NoError(t, store.Add(ws))

	err := store.Delete(ws.ID)
	require.NoError(t, err)

	_, err = store.GetByID(ws.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestWorkspaceStore_Delete_NotFound(t *testing.T) {
	store := setupWorkspaceStore(t)

	err := store.Delete(99999)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestWorkspaceStore_Delete_ZeroID(t *testing.T) {
	store := setupWorkspaceStore(t)

	err := store.Delete(0)
	require.Error(t, err)
}

func TestWorkspaceStore_SetHidden(t *testing.T) {
	store := setupWorkspaceStore(t)

	ws := &Workspace{Name: "test", Path: "/path/to/test"}
	require.NoError(t, store.Add(ws))
	require.False(t, ws.IsHidden)

	err := store.SetHidden(ws.ID, true)
	require.NoError(t, err)

	retrieved, err := store.GetByID(ws.ID)
	require.NoError(t, err)
	require.True(t, retrieved.IsHidden)

	err = store.SetHidden(ws.ID, false)
	require.NoError(t, err)

	retrieved, err = store.GetByID(ws.ID)
	require.NoError(t, err)
	require.False(t, retrieved.IsHidden)
}

func TestWorkspaceStore_SetHidden_NotFound(t *testing.T) {
	store := setupWorkspaceStore(t)

	err := store.SetHidden(99999, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestWorkspaceStore_RemoveWorkspaceFromConfig(t *testing.T) {
	store, _ := setupWorkspaceStoreWithFullConfig(t, nil, []string{"/some/path", "/other/path"})

	store.RemoveWorkspaceFromConfig("/some/path")

	cfg, err := store.loadConfig()
	require.NoError(t, err)
	require.Equal(t, []string{"/other/path"}, cfg.Workspaces)
}

func TestWorkspaceStore_RemoveWorkspaceFromConfig_NotInConfig(t *testing.T) {
	store, _ := setupWorkspaceStoreWithFullConfig(t, nil, []string{"/some/path"})

	store.RemoveWorkspaceFromConfig("/nonexistent/path")

	cfg, err := store.loadConfig()
	require.NoError(t, err)
	require.Equal(t, []string{"/some/path"}, cfg.Workspaces)
}

func TestWorkspaceStore_OnAppStart_MergesDiscoveredWithPersisted(t *testing.T) {
	rootDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(rootDir, "project-alpha"), 0755))

	store, _ := setupWorkspaceStoreWithConfig(t, []string{rootDir})

	ctx := context.Background()
	err := store.OnAppStart(ctx)
	require.NoError(t, err)

	workspaces := store.List()
	require.Len(t, workspaces, 1)
	wsID := workspaces[0].ID

	now := time.Now()
	workspaces[0].LastUsedAt = now
	require.NoError(t, store.Update(&workspaces[0]))

	err = store.OnAppStart(ctx)
	require.NoError(t, err)

	retrieved, err := store.GetByID(wsID)
	require.NoError(t, err)
	require.Equal(t, now.Unix(), retrieved.LastUsedAt.Unix())
}
