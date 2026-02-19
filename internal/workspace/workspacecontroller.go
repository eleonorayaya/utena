package workspace

import (
	"fmt"
	"net/http"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type WorkspaceController struct {
	service    *WorkspaceService
	gitService *git.GitService
}

func NewWorkspaceController(service *WorkspaceService, gitService *git.GitService) *WorkspaceController {
	return &WorkspaceController{
		service:    service,
		gitService: gitService,
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

func (c *WorkspaceController) ListBranches(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	ws, err := c.service.GetWorkspace(ctx, id)
	if err != nil {
		render.Render(w, r, common.ErrNotFound())
		return
	}

	if !ws.IsGitRepo {
		render.Render(w, r, common.ErrInvalidRequest(fmt.Errorf("workspace is not a git repository")))
		return
	}

	branches, err := c.gitService.ListBranches(ctx, ws.Path)
	if err != nil {
		render.Render(w, r, common.ErrUnknown(err))
		return
	}

	render.JSON(w, r, BranchListResponse{Branches: branches})
}
