package todo

import (
	"net/http"
	"strconv"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type TodoController struct {
	service *TodoService
}

func NewTodoController(service *TodoService) *TodoController {
	return &TodoController{
		service: service,
	}
}

func (c *TodoController) ListTodos(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	todos, err := c.service.List(ctx)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	response := NewTodoListResponse(todos)
	common.RenderResponse(w, r, response)
}

func (c *TodoController) CreateTodo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	data := &CreateTodoRequest{}
	if err := render.Bind(r, data); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	t, err := c.service.Create(ctx, data.Name, data.Description, data.WorkspaceID)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	render.Status(r, http.StatusCreated)
	common.RenderResponse(w, r, NewTodoResponse(t))
}

func (c *TodoController) DeleteTodo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	if err := c.service.Delete(ctx, uint(id)); err != nil {
		common.RenderError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
