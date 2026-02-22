package todo

import (
	"github.com/go-chi/chi/v5"
)

type TodoRouter struct {
	controller *TodoController
}

func NewTodoRouter(controller *TodoController) *TodoRouter {
	return &TodoRouter{
		controller: controller,
	}
}

func (tr *TodoRouter) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", tr.controller.ListTodos)
	r.Post("/", tr.controller.CreateTodo)
	r.Delete("/{id}", tr.controller.DeleteTodo)

	return r
}
