package todo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) db.Database {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	require.NoError(t, database.Migrate(&workspace.Workspace{}, &Todo{}))
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Logf("close database: %v", err)
		}
		if err := os.Remove(dbPath); err != nil {
			t.Logf("remove db file: %v", err)
		}
	})
	return database
}

func seedWorkspaces(t *testing.T, database db.Database) (uint, uint) {
	t.Helper()
	ws1 := &workspace.Workspace{Name: "workspace-1", Path: "/tmp/ws1"}
	ws2 := &workspace.Workspace{Name: "workspace-2", Path: "/tmp/ws2"}
	database.Create(ws1)
	database.Create(ws2)
	return ws1.ID, ws2.ID
}

func setupTodoStore(t *testing.T) (*TodoStore, uint, uint) {
	t.Helper()
	database := setupTestDB(t)
	ws1ID, ws2ID := seedWorkspaces(t, database)
	return NewTodoStore(database), ws1ID, ws2ID
}

func TestNewTodoStore(t *testing.T) {
	store, _, _ := setupTodoStore(t)
	require.NotNil(t, store)
}

func TestTodoStore_Add(t *testing.T) {
	store, ws1ID, _ := setupTodoStore(t)

	td := &Todo{
		Name:        "fix bug",
		Description: "fix the login bug",
		WorkspaceID: &ws1ID,
	}

	err := store.Add(td)
	require.NoError(t, err)
	require.NotZero(t, td.ID)

	retrieved, err := store.GetByID(td.ID)
	require.NoError(t, err)
	require.Equal(t, td.ID, retrieved.ID)
	require.Equal(t, td.Name, retrieved.Name)
	require.Equal(t, td.Description, retrieved.Description)
	require.NotNil(t, retrieved.WorkspaceID)
	require.Equal(t, ws1ID, *retrieved.WorkspaceID)
}

func TestTodoStore_Add_NilTodo(t *testing.T) {
	store, _, _ := setupTodoStore(t)
	err := store.Add(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
}

func TestTodoStore_GetByID(t *testing.T) {
	store, ws1ID, _ := setupTodoStore(t)

	td := &Todo{
		Name:        "fix bug",
		WorkspaceID: &ws1ID,
	}
	require.NoError(t, store.Add(td))

	retrieved, err := store.GetByID(td.ID)
	require.NoError(t, err)
	require.Equal(t, td.ID, retrieved.ID)
	require.Equal(t, td.Name, retrieved.Name)
}

func TestTodoStore_GetByID_NotFound(t *testing.T) {
	store, _, _ := setupTodoStore(t)

	_, err := store.GetByID(99999)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTodoNotFound))
}

func TestTodoStore_GetByID_ReturnsCopy(t *testing.T) {
	store, _, _ := setupTodoStore(t)

	td := &Todo{Name: "original"}
	require.NoError(t, store.Add(td))

	retrieved, _ := store.GetByID(td.ID)
	retrieved.Name = "modified"

	original, _ := store.GetByID(td.ID)
	require.Equal(t, "original", original.Name)
}

func TestTodoStore_List(t *testing.T) {
	store, _, _ := setupTodoStore(t)

	td1 := &Todo{Name: "oldest"}
	td2 := &Todo{Name: "newest"}
	td3 := &Todo{Name: "middle"}

	require.NoError(t, store.Add(td1))
	require.NoError(t, store.Add(td2))
	require.NoError(t, store.Add(td3))

	list := store.List()
	require.Len(t, list, 3)

	require.Equal(t, "middle", list[0].Name)
	require.Equal(t, "newest", list[1].Name)
	require.Equal(t, "oldest", list[2].Name)
}

func TestTodoStore_List_Empty(t *testing.T) {
	store, _, _ := setupTodoStore(t)
	list := store.List()
	require.Empty(t, list)
}

func TestTodoStore_ListByWorkspaceID(t *testing.T) {
	store, ws1ID, ws2ID := setupTodoStore(t)

	td1 := &Todo{Name: "ws1-old", WorkspaceID: &ws1ID}
	td2 := &Todo{Name: "ws2", WorkspaceID: &ws2ID}
	td3 := &Todo{Name: "ws1-new", WorkspaceID: &ws1ID}

	require.NoError(t, store.Add(td1))
	require.NoError(t, store.Add(td2))
	require.NoError(t, store.Add(td3))

	ws1Todos := store.ListByWorkspaceID(ws1ID)
	require.Len(t, ws1Todos, 2)

	for _, td := range ws1Todos {
		require.NotNil(t, td.WorkspaceID)
		require.Equal(t, ws1ID, *td.WorkspaceID)
	}

	require.Equal(t, "ws1-new", ws1Todos[0].Name)
	require.Equal(t, "ws1-old", ws1Todos[1].Name)

	ws2Todos := store.ListByWorkspaceID(ws2ID)
	require.Len(t, ws2Todos, 1)
	require.Equal(t, "ws2", ws2Todos[0].Name)
}

func TestTodoStore_ListByWorkspaceID_Empty(t *testing.T) {
	store, _, _ := setupTodoStore(t)
	list := store.ListByWorkspaceID(99999)
	require.Empty(t, list)
}

func TestTodoStore_Delete(t *testing.T) {
	store, _, _ := setupTodoStore(t)

	td := &Todo{Name: "test"}
	require.NoError(t, store.Add(td))

	err := store.Delete(td.ID)
	require.NoError(t, err)

	_, err = store.GetByID(td.ID)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTodoNotFound))
}

func TestTodoStore_Delete_NotFound(t *testing.T) {
	store, _, _ := setupTodoStore(t)

	err := store.Delete(99999)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTodoNotFound))
}

func TestTodoStore_Delete_ZeroID(t *testing.T) {
	store, _, _ := setupTodoStore(t)

	err := store.Delete(0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ID cannot be zero")
}

func TestTodoStore_Persistence(t *testing.T) {
	database := setupTestDB(t)
	ws1ID, _ := seedWorkspaces(t, database)

	store1 := NewTodoStore(database)
	td := &Todo{Name: "persist me", WorkspaceID: &ws1ID}
	require.NoError(t, store1.Add(td))

	store2 := NewTodoStore(database)

	list := store2.List()
	require.Len(t, list, 1)
	require.Equal(t, td.ID, list[0].ID)
	require.Equal(t, "persist me", list[0].Name)
}

func TestTodoStore_ConcurrentAccess(t *testing.T) {
	store, _, _ := setupTodoStore(t)

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			td := &Todo{
				Name: "concurrent",
			}
			require.NoError(t, store.Add(td))
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

func TestTodoStore_OnAppStart_NoFile(t *testing.T) {
	store, _, _ := setupTodoStore(t)
	err := store.OnAppStart(context.Background())
	require.NoError(t, err)
}

func TestTodoStore_OnAppEnd(t *testing.T) {
	store, _, _ := setupTodoStore(t)
	err := store.OnAppEnd(context.Background())
	require.NoError(t, err)
}
