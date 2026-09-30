package tmux

import (
	"github.com/go-chi/chi/v5"
)

type TmuxRouter struct {
	controller *TmuxController
}

func NewTmuxRouter(controller *TmuxController) *TmuxRouter {
	return &TmuxRouter{
		controller: controller,
	}
}

func (tr *TmuxRouter) Routes() chi.Router {
	r := chi.NewRouter()

	r.Put("/hooks/{event}", tr.controller.HandleHook)
	r.Get("/windows/{sessionName}", tr.controller.HandleGetWindows)
	r.Get("/sessions/{sessionName}/env/{key}", tr.controller.HandleGetSessionEnv)
	r.Get("/activations/next", tr.controller.HandleNextActivation)

	return r
}
