package workspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/eleonorayaya/utena/internal/claudesettings"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// SessionCleaner removes session/worktree DB rows that reference a repo,
// so the bare-workspace migration can drop the worktree records without
// hitting the RESTRICT foreign key on session_worktrees.worktree_id. The
// session module owns this logic; the workspace module receives an
// implementation via SetSessionCleaner after both modules are constructed
// (session imports workspace, so the dependency has to flow this way).
type SessionCleaner interface {
	DetachWorktreesByRepoID(ctx context.Context, repoID uint) error
}

type WorkspaceController struct {
	service        *WorkspaceService
	gitService     *git.GitService
	sessionCleaner SessionCleaner
}

func NewWorkspaceController(service *WorkspaceService, gitService *git.GitService) *WorkspaceController {
	return &WorkspaceController{
		service:    service,
		gitService: gitService,
	}
}

func (c *WorkspaceController) SetSessionCleaner(cleaner SessionCleaner) {
	c.sessionCleaner = cleaner
}

func (c *WorkspaceController) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	workspaces, err := c.service.ListWorkspaces(ctx)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	response := NewWorkspaceListResponse(workspaces)
	common.RenderResponse(w, r, response)
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
	common.RenderResponse(w, r, response)
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
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			common.RenderError(w, r, err)
		} else {
			common.RenderError(w, r, common.WrapInvalidRequest("add workspace failed", err))
		}
		return
	}

	render.Status(r, http.StatusCreated)
	if ws != nil {
		common.RenderResponse(w, r, NewWorkspaceListResponse([]Workspace{*ws}))
	} else {
		workspaces, listErr := c.service.ListWorkspaces(ctx)
		if listErr != nil {
			common.RenderError(w, r, listErr)
			return
		}
		common.RenderResponse(w, r, NewWorkspaceListResponse(workspaces))
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

func (c *WorkspaceController) MigrateWorkspaceToBare(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Minute)
	defer cancel()
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

	if ws.IsBare {
		common.RenderError(w, r, common.NewInvalidRequest(fmt.Sprintf("workspace %q is already using the bare pattern", ws.Name)))
		return
	}

	if ws.RepoID != nil {
		if c.sessionCleaner == nil {
			common.RenderError(w, r, fmt.Errorf("session cleaner not configured; cannot safely clear worktrees for repo %d", *ws.RepoID))
			return
		}
		if err := c.sessionCleaner.DetachWorktreesByRepoID(ctx, *ws.RepoID); err != nil {
			common.RenderError(w, r, fmt.Errorf("failed to clear worktree records before bare migration: %w", err))
			return
		}
	}

	if err := c.gitService.MigrateToBare(ctx, ws.Path); err != nil {
		common.RenderError(w, r, err)
		return
	}

	if err := c.service.MarkAsBare(ctx, uint(id)); err != nil {
		common.RenderError(w, r, err)
		return
	}

	if err := claudesettings.EnsureWorkspaceRoot(ws.Path); err != nil {
		slog.Warn("failed to bootstrap claude settings after bare migration",
			"workspace", ws.Name, "error", err)
	}

	render.NoContent(w, r)
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
