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
	r.Post("/{id}/workspaces", sr.controller.AddWorkspace)
	r.Put("/{id}/activate", sr.controller.ActivateSession)
	r.Put("/{id}/repair", sr.controller.RepairSession)
	r.Put("/{id}/archive", sr.controller.ArchiveSession)
	r.Put("/{id}/dismiss", sr.controller.DismissSession)
	r.Get("/{id}", sr.controller.GetSessionByID)
	r.Put("/{id}", sr.controller.UpdateSession)
	r.Delete("/{id}", sr.controller.DeleteSession)

	return r
}
