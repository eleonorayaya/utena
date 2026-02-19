package claude

import (
	"github.com/go-chi/chi/v5"
)

type ClaudeRouter struct {
	controller *ClaudeController
}

func NewClaudeRouter(controller *ClaudeController) *ClaudeRouter {
	return &ClaudeRouter{
		controller: controller,
	}
}

func (cr *ClaudeRouter) Routes() chi.Router {
	r := chi.NewRouter()

	r.Put("/hook", cr.controller.HandleHookEvent)
	r.Get("/sessions", cr.controller.ListClaudeSessions)
	r.Get("/sessions/{sessionId}", cr.controller.ListClaudeSessionsBySession)

	return r
}
