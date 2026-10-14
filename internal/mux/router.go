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

func (rt *Router) Routes() chi.Router {
	r := chi.NewRouter()

	r.Put("/hooks/{event}", rt.controller.HandleHook)
	r.Get("/windows/{sessionName}", rt.controller.HandleGetWindows)
	r.Get("/sessions/{sessionName}/env/{key}", rt.controller.HandleGetSessionEnv)
	r.Get("/activations/next", rt.controller.HandleNextActivation)

	return r
}
