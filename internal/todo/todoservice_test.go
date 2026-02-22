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

	fs := afero.NewMemMapFs()
	todoStore := NewTodoStore(fs, "/config")
	workspaceStore := workspace.NewWorkspaceStore(afero.NewMemMapFs(), "/config")
	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	service := NewTodoService(todoStore, workspaceService)

	return service, todoStore, workspaceStore
}

func TestTodoService_Create(t *testing.T) {
	service, _, workspaceStore := setupTodoService(t)
	workspaceStore.Add(&workspace.Workspace{ID: "ws-1", Name: "utena", Path: "/tmp/utena"})

	ctx := context.Background()
	td, err := service.Create(ctx, "fix bug", "fix the login bug", "ws-1")
	require.NoError(t, err)
	require.NotEmpty(t, td.ID)
	require.Equal(t, "fix bug", td.Name)
	require.Equal(t, "fix the login bug", td.Description)
	require.Equal(t, "ws-1", td.WorkspaceID)
	require.Equal(t, "utena", td.WorkspaceName)
	require.False(t, td.CreatedAt.IsZero())
}

func TestTodoService_Create_NoWorkspace(t *testing.T) {
	service, _, _ := setupTodoService(t)

	ctx := context.Background()
	td, err := service.Create(ctx, "general task", "no workspace", "")
	require.NoError(t, err)
	require.NotEmpty(t, td.ID)
	require.Equal(t, "general task", td.Name)
	require.Empty(t, td.WorkspaceID)
	require.Empty(t, td.WorkspaceName)
}

func TestTodoService_Create_InvalidWorkspace(t *testing.T) {
	service, _, _ := setupTodoService(t)

	ctx := context.Background()
	_, err := service.Create(ctx, "task", "desc", "nonexistent")
	require.Error(t, err)
}

func TestTodoService_List(t *testing.T) {
	service, _, workspaceStore := setupTodoService(t)
	workspaceStore.Add(&workspace.Workspace{ID: "ws-1", Name: "utena", Path: "/tmp/utena"})

	ctx := context.Background()
	service.Create(ctx, "task 1", "", "ws-1")
	service.Create(ctx, "task 2", "", "")

	todos, err := service.List(ctx)
	require.NoError(t, err)
	require.Len(t, todos, 2)
}

func TestTodoService_List_ResolvesWorkspaceNames(t *testing.T) {
	service, _, workspaceStore := setupTodoService(t)
	workspaceStore.Add(&workspace.Workspace{ID: "ws-1", Name: "utena", Path: "/tmp/utena"})

	ctx := context.Background()
	service.Create(ctx, "task 1", "", "ws-1")

	todos, err := service.List(ctx)
	require.NoError(t, err)
	require.Len(t, todos, 1)
	require.Equal(t, "utena", todos[0].WorkspaceName)
}

func TestTodoService_ListByWorkspace(t *testing.T) {
	service, _, workspaceStore := setupTodoService(t)
	workspaceStore.Add(&workspace.Workspace{ID: "ws-1", Name: "utena", Path: "/tmp/utena"})
	workspaceStore.Add(&workspace.Workspace{ID: "ws-2", Name: "other", Path: "/tmp/other"})

	ctx := context.Background()
	service.Create(ctx, "task 1", "", "ws-1")
	service.Create(ctx, "task 2", "", "ws-2")
	service.Create(ctx, "task 3", "", "ws-1")

	ws1Todos, err := service.ListByWorkspace(ctx, "ws-1")
	require.NoError(t, err)
	require.Len(t, ws1Todos, 2)
	for _, td := range ws1Todos {
		require.Equal(t, "ws-1", td.WorkspaceID)
	}

	ws2Todos, err := service.ListByWorkspace(ctx, "ws-2")
	require.NoError(t, err)
	require.Len(t, ws2Todos, 1)
}

func TestTodoService_Delete(t *testing.T) {
	service, _, workspaceStore := setupTodoService(t)
	workspaceStore.Add(&workspace.Workspace{ID: "ws-1", Name: "utena", Path: "/tmp/utena"})

	ctx := context.Background()
	td, _ := service.Create(ctx, "task", "", "ws-1")

	err := service.Delete(ctx, td.ID)
	require.NoError(t, err)

	todos, _ := service.List(ctx)
	require.Empty(t, todos)
}

func TestTodoService_Delete_NotFound(t *testing.T) {
	service, _, _ := setupTodoService(t)

	ctx := context.Background()
	err := service.Delete(ctx, "nonexistent")
	require.Error(t, err)
}

func TestTodoService_UniqueIDs(t *testing.T) {
	service, _, _ := setupTodoService(t)

	ctx := context.Background()
	td1, _ := service.Create(ctx, "same name", "", "")
	td2, _ := service.Create(ctx, "same name", "", "")

	require.NotEqual(t, td1.ID, td2.ID)
}
