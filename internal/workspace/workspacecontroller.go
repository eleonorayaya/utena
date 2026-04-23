package workspace

import (
	"fmt"
	"net/http"
	"strconv"

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
		common.RenderError(w, r, err)
		return
	}

	response := NewWorkspaceListResponse(workspaces)
	render.Render(w, r, response)
}

func (c *WorkspaceController) GetWorkspaceByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	workspace, err := c.service.GetWorkspace(ctx, uint(id))
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	response := NewWorkspaceResponse(workspace)
	render.Render(w, r, response)
}

func (c *WorkspaceController) AddWorkspace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req AddWorkspaceRequest
	if err := render.Bind(r, &req); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	ws, err := c.service.AddWorkspace(ctx, req.Path, req.AsRoot)
	if err != nil {
		common.RenderError(w, r, common.WrapInvalidRequest("add workspace failed", err))
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

func (c *WorkspaceController) SetWorkspaceHidden(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	var req SetHiddenRequest
	if err := render.Bind(r, &req); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	if err := c.service.SetWorkspaceHidden(ctx, uint(id), req.Hidden); err != nil {
		common.RenderError(w, r, err)
		return
	}

	render.NoContent(w, r)
}

func (c *WorkspaceController) DeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	if err := c.service.DeleteWorkspace(ctx, uint(id)); err != nil {
		common.RenderError(w, r, err)
		return
	}

	render.NoContent(w, r)
}

func (c *WorkspaceController) ListBranches(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	ws, err := c.service.GetWorkspace(ctx, uint(id))
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	if !ws.IsGitRepo {
		common.RenderError(w, r, common.NewInvalidRequest(fmt.Sprintf("workspace %q is not a git repository", ws.Name)))
		return
	}

	branches, err := c.gitService.ListBranches(ctx, ws.Path)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	currentBranch, err := c.gitService.CurrentBranch(ctx, ws.Path)
	if err != nil {
		currentBranch = ""
	}

	render.JSON(w, r, BranchListResponse{Branches: branches, CurrentBranch: currentBranch})
}

func (c *WorkspaceController) FetchAndListBranches(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	ws, err := c.service.GetWorkspace(ctx, uint(id))
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	if !ws.IsGitRepo {
		common.RenderError(w, r, common.NewInvalidRequest(fmt.Sprintf("workspace %q is not a git repository", ws.Name)))
		return
	}

	if err := c.gitService.FetchOrigin(ctx, ws.Path); err != nil {
		common.RenderError(w, r, err)
		return
	}

	branches, err := c.gitService.ListAllBranches(ctx, ws.Path)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	currentBranch, err := c.gitService.CurrentBranch(ctx, ws.Path)
	if err != nil {
		currentBranch = ""
	}

	render.JSON(w, r, BranchRefListResponse{Branches: branches, CurrentBranch: currentBranch})
}

func (c *WorkspaceController) CheckBranchExists(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		common.RenderError(w, r, common.NewInvalidRequest("name query parameter is required"))
		return
	}

	ws, err := c.service.GetWorkspace(ctx, uint(id))
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	if !ws.IsGitRepo {
		common.RenderError(w, r, common.NewInvalidRequest(fmt.Sprintf("workspace %q is not a git repository", ws.Name)))
		return
	}

	existsLocal, err := c.gitService.HasBranch(ctx, ws.Path, name)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	existsRemote, err := c.gitService.HasRemoteBranch(ctx, ws.Path, name)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	render.JSON(w, r, BranchExistsResponse{ExistsLocal: existsLocal, ExistsRemote: existsRemote})
}

func (c *WorkspaceController) ListPRs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	ws, err := c.service.GetWorkspace(ctx, uint(id))
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	if ws.RepoID == nil {
		common.RenderError(w, r, common.NewInvalidRequest("workspace has no associated repository"))
		return
	}

	state := git.PRState(r.URL.Query().Get("state"))
	prs := c.gitService.SearchPRs(*ws.RepoID, state)
	render.JSON(w, r, PRListResponse{PullRequests: prs})
}
