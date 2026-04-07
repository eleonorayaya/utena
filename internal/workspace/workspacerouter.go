package workspace

import (
	"github.com/go-chi/chi/v5"
)

type WorkspaceRouter struct {
	controller *WorkspaceController
}

func NewWorkspaceRouter(controller *WorkspaceController) *WorkspaceRouter {
	return &WorkspaceRouter{
		controller: controller,
	}
}

func (wr *WorkspaceRouter) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", wr.controller.ListWorkspaces)
	r.Post("/", wr.controller.AddWorkspace)
	r.Get("/{id}/branches", wr.controller.ListBranches)
	r.Get("/{id}/prs", wr.controller.ListPRs)
	r.Get("/{id}", wr.controller.GetWorkspaceByID)

	return r
}
