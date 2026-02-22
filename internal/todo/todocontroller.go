package todo

import (
	"net/http"

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
		render.Render(w, r, common.ErrUnknown(err))
		return
	}

	response := NewTodoListResponse(todos)
	render.Render(w, r, response)
}

func (c *TodoController) CreateTodo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	data := &CreateTodoRequest{}
	if err := render.Bind(r, data); err != nil {
		render.Render(w, r, common.ErrInvalidRequest(err))
		return
	}

	t, err := c.service.Create(ctx, data.Name, data.Description, data.WorkspaceID)
	if err != nil {
		render.Render(w, r, common.ErrUnknown(err))
		return
	}

	render.Status(r, http.StatusCreated)
	render.Render(w, r, NewTodoResponse(t))
}

func (c *TodoController) DeleteTodo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	if err := c.service.Delete(ctx, id); err != nil {
		if err == ErrTodoNotFound {
			render.Render(w, r, common.ErrNotFound())
			return
		}
		render.Render(w, r, common.ErrUnknown(err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
