package todo

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupTodoStore(t *testing.T) *TodoStore {
	t.Helper()
	return NewTodoStore(afero.NewMemMapFs(), "/config")
}

func TestNewTodoStore(t *testing.T) {
	store := setupTodoStore(t)
	require.NotNil(t, store)
	require.NotNil(t, store.todos)
}

func TestTodoStore_Add(t *testing.T) {
	store := setupTodoStore(t)

	td := &Todo{
		ID:          "todo-1",
		Name:        "fix bug",
		Description: "fix the login bug",
		WorkspaceID: "ws-1",
		CreatedAt:   time.Now(),
	}

	err := store.Add(td)
	require.NoError(t, err)

	retrieved, err := store.GetByID("todo-1")
	require.NoError(t, err)
	require.Equal(t, td.ID, retrieved.ID)
	require.Equal(t, td.Name, retrieved.Name)
	require.Equal(t, td.Description, retrieved.Description)
	require.Equal(t, td.WorkspaceID, retrieved.WorkspaceID)
}

func TestTodoStore_Add_NilTodo(t *testing.T) {
	store := setupTodoStore(t)
	err := store.Add(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
}

func TestTodoStore_Add_EmptyID(t *testing.T) {
	store := setupTodoStore(t)
	td := &Todo{Name: "test", CreatedAt: time.Now()}
	err := store.Add(td)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ID cannot be empty")
}

func TestTodoStore_GetByID(t *testing.T) {
	store := setupTodoStore(t)

	td := &Todo{
		ID:          "todo-1",
		Name:        "fix bug",
		WorkspaceID: "ws-1",
		CreatedAt:   time.Now(),
	}
	store.Add(td)

	retrieved, err := store.GetByID("todo-1")
	require.NoError(t, err)
	require.Equal(t, td.ID, retrieved.ID)
	require.Equal(t, td.Name, retrieved.Name)
}

func TestTodoStore_GetByID_NotFound(t *testing.T) {
	store := setupTodoStore(t)

	_, err := store.GetByID("nonexistent")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTodoNotFound))
}

func TestTodoStore_GetByID_ReturnsCopy(t *testing.T) {
	store := setupTodoStore(t)

	td := &Todo{ID: "todo-1", Name: "original", CreatedAt: time.Now()}
	store.Add(td)

	retrieved, _ := store.GetByID("todo-1")
	retrieved.Name = "modified"

	original, _ := store.GetByID("todo-1")
	require.Equal(t, "original", original.Name)
}

func TestTodoStore_List(t *testing.T) {
	store := setupTodoStore(t)

	now := time.Now()
	td1 := &Todo{ID: "todo-1", Name: "oldest", CreatedAt: now.Add(-2 * time.Hour)}
	td2 := &Todo{ID: "todo-2", Name: "newest", CreatedAt: now}
	td3 := &Todo{ID: "todo-3", Name: "middle", CreatedAt: now.Add(-1 * time.Hour)}

	store.Add(td1)
	store.Add(td2)
	store.Add(td3)

	list := store.List()
	require.Len(t, list, 3)

	require.Equal(t, "todo-2", list[0].ID)
	require.Equal(t, "todo-3", list[1].ID)
	require.Equal(t, "todo-1", list[2].ID)
}

func TestTodoStore_List_Empty(t *testing.T) {
	store := setupTodoStore(t)
	list := store.List()
	require.Empty(t, list)
}

func TestTodoStore_ListByWorkspaceID(t *testing.T) {
	store := setupTodoStore(t)

	now := time.Now()
	td1 := &Todo{ID: "todo-1", Name: "ws1-old", WorkspaceID: "ws-1", CreatedAt: now.Add(-2 * time.Hour)}
	td2 := &Todo{ID: "todo-2", Name: "ws2", WorkspaceID: "ws-2", CreatedAt: now}
	td3 := &Todo{ID: "todo-3", Name: "ws1-new", WorkspaceID: "ws-1", CreatedAt: now.Add(-1 * time.Hour)}

	store.Add(td1)
	store.Add(td2)
	store.Add(td3)

	ws1Todos := store.ListByWorkspaceID("ws-1")
	require.Len(t, ws1Todos, 2)

	for _, td := range ws1Todos {
		require.Equal(t, "ws-1", td.WorkspaceID)
	}

	require.Equal(t, "todo-3", ws1Todos[0].ID)
	require.Equal(t, "todo-1", ws1Todos[1].ID)

	ws2Todos := store.ListByWorkspaceID("ws-2")
	require.Len(t, ws2Todos, 1)
	require.Equal(t, "todo-2", ws2Todos[0].ID)
}

func TestTodoStore_ListByWorkspaceID_Empty(t *testing.T) {
	store := setupTodoStore(t)
	list := store.ListByWorkspaceID("nonexistent")
	require.Empty(t, list)
}

func TestTodoStore_Delete(t *testing.T) {
	store := setupTodoStore(t)

	td := &Todo{ID: "todo-1", Name: "test", CreatedAt: time.Now()}
	store.Add(td)

	err := store.Delete("todo-1")
	require.NoError(t, err)

	_, err = store.GetByID("todo-1")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTodoNotFound))
}

func TestTodoStore_Delete_NotFound(t *testing.T) {
	store := setupTodoStore(t)

	err := store.Delete("nonexistent")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTodoNotFound))
}

func TestTodoStore_Delete_EmptyID(t *testing.T) {
	store := setupTodoStore(t)

	err := store.Delete("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ID cannot be empty")
}

func TestTodoStore_Persistence(t *testing.T) {
	fs := afero.NewMemMapFs()
	configDir := "/config"

	store1 := NewTodoStore(fs, configDir)
	td := &Todo{ID: "todo-1", Name: "persist me", WorkspaceID: "ws-1", CreatedAt: time.Now()}
	store1.Add(td)

	store2 := NewTodoStore(fs, configDir)
	err := store2.OnAppStart(context.Background())
	require.NoError(t, err)

	list := store2.List()
	require.Len(t, list, 1)
	require.Equal(t, "todo-1", list[0].ID)
	require.Equal(t, "persist me", list[0].Name)
}

func TestTodoStore_ConcurrentAccess(t *testing.T) {
	store := setupTodoStore(t)

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			td := &Todo{
				ID:        string(rune('a' + id)),
				Name:      "concurrent",
				CreatedAt: time.Now(),
			}
			store.Add(td)
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
	store := setupTodoStore(t)
	err := store.OnAppStart(context.Background())
	require.NoError(t, err)
}

func TestTodoStore_OnAppEnd(t *testing.T) {
	store := setupTodoStore(t)
	err := store.OnAppEnd(context.Background())
	require.NoError(t, err)
}
