package todo

import (
	"context"
	"testing"

	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupTodoService(t *testing.T) (*TodoService, *TodoStore, *workspace.WorkspaceStore) {
	t.Helper()

	database := setupTestDB(t)
	todoStore := NewTodoStore(database)
	workspaceStore := workspace.NewWorkspaceStore(database, afero.NewMemMapFs(), "/config")
	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	service := NewTodoService(todoStore, workspaceService)

	return service, todoStore, workspaceStore
}

func TestTodoService_Create(t *testing.T) {
	service, _, workspaceStore := setupTodoService(t)
	ws := &workspace.Workspace{Name: "utena", Path: "/tmp/utena"}
	require.NoError(t, workspaceStore.Add(ws))

	ctx := context.Background()
	td, err := service.Create(ctx, "fix bug", "fix the login bug", &ws.ID)
	require.NoError(t, err)
	require.NotZero(t, td.ID)
	require.Equal(t, "fix bug", td.Name)
	require.Equal(t, "fix the login bug", td.Description)
	require.NotNil(t, td.WorkspaceID)
	require.Equal(t, ws.ID, *td.WorkspaceID)
	require.NotNil(t, td.Workspace)
	require.Equal(t, "utena", td.Workspace.Name)
	require.False(t, td.CreatedAt.IsZero())
}

func TestTodoService_Create_NoWorkspace(t *testing.T) {
	service, _, _ := setupTodoService(t)

	ctx := context.Background()
	td, err := service.Create(ctx, "general task", "no workspace", nil)
	require.NoError(t, err)
	require.NotZero(t, td.ID)
	require.Equal(t, "general task", td.Name)
	require.Nil(t, td.WorkspaceID)
	require.Nil(t, td.Workspace)
}

func TestTodoService_Create_InvalidWorkspace(t *testing.T) {
	service, _, _ := setupTodoService(t)

	ctx := context.Background()
	badID := uint(99999)
	_, err := service.Create(ctx, "task", "desc", &badID)
	require.Error(t, err)
}

func TestTodoService_List(t *testing.T) {
	service, _, workspaceStore := setupTodoService(t)
	ws := &workspace.Workspace{Name: "utena", Path: "/tmp/utena"}
	require.NoError(t, workspaceStore.Add(ws))

	ctx := context.Background()
	_, err := service.Create(ctx, "task 1", "", &ws.ID)
	require.NoError(t, err)
	_, err = service.Create(ctx, "task 2", "", nil)
	require.NoError(t, err)

	todos, err := service.List(ctx)
	require.NoError(t, err)
	require.Len(t, todos, 2)
}

func TestTodoService_List_ResolvesWorkspaceNames(t *testing.T) {
	service, _, workspaceStore := setupTodoService(t)
	ws := &workspace.Workspace{Name: "utena", Path: "/tmp/utena"}
	require.NoError(t, workspaceStore.Add(ws))

	ctx := context.Background()
	_, err := service.Create(ctx, "task 1", "", &ws.ID)
	require.NoError(t, err)

	todos, err := service.List(ctx)
	require.NoError(t, err)
	require.Len(t, todos, 1)
	require.NotNil(t, todos[0].Workspace)
	require.Equal(t, "utena", todos[0].Workspace.Name)
}

func TestTodoService_ListByWorkspace(t *testing.T) {
	service, _, workspaceStore := setupTodoService(t)
	ws1 := &workspace.Workspace{Name: "utena", Path: "/tmp/utena"}
	ws2 := &workspace.Workspace{Name: "other", Path: "/tmp/other"}
	require.NoError(t, workspaceStore.Add(ws1))
	require.NoError(t, workspaceStore.Add(ws2))

	ctx := context.Background()
	_, err := service.Create(ctx, "task 1", "", &ws1.ID)
	require.NoError(t, err)
	_, err = service.Create(ctx, "task 2", "", &ws2.ID)
	require.NoError(t, err)
	_, err = service.Create(ctx, "task 3", "", &ws1.ID)
	require.NoError(t, err)

	ws1Todos, err := service.ListByWorkspace(ctx, ws1.ID)
	require.NoError(t, err)
	require.Len(t, ws1Todos, 2)
	for _, td := range ws1Todos {
		require.NotNil(t, td.WorkspaceID)
		require.Equal(t, ws1.ID, *td.WorkspaceID)
	}

	ws2Todos, err := service.ListByWorkspace(ctx, ws2.ID)
	require.NoError(t, err)
	require.Len(t, ws2Todos, 1)
}

func TestTodoService_Delete(t *testing.T) {
	service, _, workspaceStore := setupTodoService(t)
	ws := &workspace.Workspace{Name: "utena", Path: "/tmp/utena"}
	require.NoError(t, workspaceStore.Add(ws))

	ctx := context.Background()
	td, _ := service.Create(ctx, "task", "", &ws.ID)

	err := service.Delete(ctx, td.ID)
	require.NoError(t, err)

	todos, _ := service.List(ctx)
	require.Empty(t, todos)
}

func TestTodoService_Delete_NotFound(t *testing.T) {
	service, _, _ := setupTodoService(t)

	ctx := context.Background()
	err := service.Delete(ctx, 99999)
	require.Error(t, err)
}

func TestTodoService_UniqueIDs(t *testing.T) {
	service, _, _ := setupTodoService(t)

	ctx := context.Background()
	td1, _ := service.Create(ctx, "same name", "", nil)
	td2, _ := service.Create(ctx, "same name", "", nil)

	require.NotEqual(t, td1.ID, td2.ID)
}
