package mux

import (
	"github.com/go-chi/chi/v5"
)

type Router struct {
	controller *Controller
}

func NewRouter(controller *Controller) *Router {
	return &Router{
		controller: controller,
	}
}

func (tr *Router) Routes() chi.Router {
	r := chi.NewRouter()

	r.Put("/hooks/{event}", tr.controller.HandleHook)
	r.Get("/windows/{sessionName}", tr.controller.HandleGetWindows)
	r.Get("/sessions/{sessionName}/env/{key}", tr.controller.HandleGetSessionEnv)
	r.Get("/activations/next", tr.controller.HandleNextActivation)

	return r
}
