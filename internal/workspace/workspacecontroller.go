package workspace

import (
	"net/http"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type WorkspaceController struct {
	service *WorkspaceService
}

func NewWorkspaceController(service *WorkspaceService) *WorkspaceController {
	return &WorkspaceController{
		service: service,
	}
}

func (c *WorkspaceController) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	workspaces, err := c.service.ListWorkspaces(ctx)
	if err != nil {
		render.Render(w, r, common.ErrUnknown(err))
		return
	}

	response := NewWorkspaceListResponse(workspaces)
	render.Render(w, r, response)
}

func (c *WorkspaceController) GetWorkspaceByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	workspace, err := c.service.GetWorkspace(ctx, id)
	if err != nil {
		render.Render(w, r, common.ErrNotFound())
		return
	}

	response := NewWorkspaceResponse(workspace)
	render.Render(w, r, response)
}

func (c *WorkspaceController) AddWorkspace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req AddWorkspaceRequest
	if err := render.Bind(r, &req); err != nil {
		render.Render(w, r, common.ErrInvalidRequest(err))
		return
	}

	ws, err := c.service.AddWorkspace(ctx, req.Path, req.AsRoot)
	if err != nil {
		render.Render(w, r, common.ErrInvalidRequest(err))
		return
	}

	render.Status(r, http.StatusCreated)
	if ws != nil {
		render.Render(w, r, NewWorkspaceListResponse([]Workspace{*ws}))
	} else {
		workspaces, _ := c.service.ListWorkspaces(ctx)
		render.Render(w, r, NewWorkspaceListResponse(workspaces))
	}
}
