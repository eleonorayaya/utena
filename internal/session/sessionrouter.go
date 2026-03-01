package session

import (
	"github.com/go-chi/chi/v5"
)

type SessionRouter struct {
	controller *SessionController
}

func NewSessionRouter(controller *SessionController) *SessionRouter {
	return &SessionRouter{
		controller: controller,
	}
}

func (sr *SessionRouter) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", sr.controller.ListSessions)
	r.Post("/", sr.controller.CreateSession)
	r.Get("/workspace/{workspaceId}", sr.controller.ListSessionsByWorkspace)
	r.Put("/{name}/activate", sr.controller.ActivateSession)
	r.Put("/{name}/revive", sr.controller.ReviveSession)
	r.Get("/{id}", sr.controller.GetSessionByID)
	r.Put("/{id}", sr.controller.UpdateSession)
	r.Delete("/{id}", sr.controller.DeleteSession)

	return r
}
