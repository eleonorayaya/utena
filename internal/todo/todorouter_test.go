package todo

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func setupTodoRouter(t *testing.T) (*TodoRouter, *TodoStore, *workspace.WorkspaceStore) {
	t.Helper()

	fs := afero.NewMemMapFs()
	todoStore := NewTodoStore(fs, "/config")
	workspaceStore := workspace.NewWorkspaceStore(afero.NewMemMapFs(), "/config")

	workspaceStore.Add(&workspace.Workspace{ID: "ws-1", Name: "utena", Path: "/tmp/utena"})
	workspaceStore.Add(&workspace.Workspace{ID: "ws-2", Name: "other", Path: "/tmp/other"})

	workspaceService := workspace.NewWorkspaceService(workspaceStore)
	service := NewTodoService(todoStore, workspaceService)
	controller := NewTodoController(service)
	router := NewTodoRouter(controller)

	return router, todoStore, workspaceStore
}

func TestTodoRouter_ListTodos(t *testing.T) {
	router, _, _ := setupTodoRouter(t)

	body1, _ := json.Marshal(CreateTodoRequest{Name: "task 1", WorkspaceID: "ws-1"})
	body2, _ := json.Marshal(CreateTodoRequest{Name: "task 2", WorkspaceID: "ws-2"})

	req1 := httptest.NewRequest("POST", "/", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	router.Routes().ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest("POST", "/", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	router.Routes().ServeHTTP(httptest.NewRecorder(), req2)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response TodoListResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Len(t, response.Todos, 2)
}

func TestTodoRouter_ListTodos_Empty(t *testing.T) {
	router, _, _ := setupTodoRouter(t)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response TodoListResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Empty(t, response.Todos)
}

func TestTodoRouter_CreateTodo(t *testing.T) {
	router, todoStore, _ := setupTodoRouter(t)

	body, _ := json.Marshal(CreateTodoRequest{
		Name:        "fix bug",
		Description: "fix the login bug",
		WorkspaceID: "ws-1",
	})

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)

	var response TodoResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "fix bug", response.Name)
	require.Equal(t, "fix the login bug", response.Description)
	require.Equal(t, "ws-1", response.WorkspaceID)
	require.Equal(t, "utena", response.WorkspaceName)
	require.NotEmpty(t, response.ID)

	retrieved, err := todoStore.GetByID(response.ID)
	require.NoError(t, err)
	require.Equal(t, "fix bug", retrieved.Name)
}

func TestTodoRouter_CreateTodo_NoWorkspace(t *testing.T) {
	router, _, _ := setupTodoRouter(t)

	body, _ := json.Marshal(CreateTodoRequest{Name: "general task"})

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)

	var response TodoResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "general task", response.Name)
	require.Empty(t, response.WorkspaceID)
}

func TestTodoRouter_CreateTodo_MissingName(t *testing.T) {
	router, _, _ := setupTodoRouter(t)

	body, _ := json.Marshal(CreateTodoRequest{Description: "no name"})

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTodoRouter_CreateTodo_InvalidWorkspace(t *testing.T) {
	router, _, _ := setupTodoRouter(t)

	body, _ := json.Marshal(CreateTodoRequest{Name: "task", WorkspaceID: "nonexistent"})

	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.Routes().ServeHTTP(w, req)

	require.NotEqual(t, http.StatusCreated, w.Code)
}

func TestTodoRouter_DeleteTodo(t *testing.T) {
	router, _, _ := setupTodoRouter(t)

	body, _ := json.Marshal(CreateTodoRequest{Name: "to delete", WorkspaceID: "ws-1"})
	createReq := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	router.Routes().ServeHTTP(createW, createReq)

	var created TodoResponse
	json.Unmarshal(createW.Body.Bytes(), &created)

	req := httptest.NewRequest("DELETE", "/"+created.ID, nil)
	w := httptest.NewRecorder()
	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)

	listReq := httptest.NewRequest("GET", "/", nil)
	listW := httptest.NewRecorder()
	router.Routes().ServeHTTP(listW, listReq)

	var listResp TodoListResponse
	json.Unmarshal(listW.Body.Bytes(), &listResp)
	require.Empty(t, listResp.Todos)
}

func TestTodoRouter_DeleteTodo_NotFound(t *testing.T) {
	router, _, _ := setupTodoRouter(t)

	req := httptest.NewRequest("DELETE", "/nonexistent", nil)
	w := httptest.NewRecorder()
	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}
