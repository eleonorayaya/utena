package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/eleonorayaya/utena/internal/claudesettings"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type bareMigrationHandler struct {
	workspaceService *workspace.WorkspaceService
	gitService       *git.GitService
	sessionService   *session.SessionService
}

func (h *bareMigrationHandler) handle(w http.ResponseWriter, r *http.Request) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	ws, err := h.workspaceService.GetWorkspace(r.Context(), uint(id))
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

	migrating, err := h.workspaceService.BeginMigration(r.Context(), uint(id))
	if err != nil {
		common.RenderError(w, r, err)
		return
	}
	go h.runMigration(uint(id), ws)

	render.Status(r, http.StatusAccepted)
	common.RenderResponse(w, r, workspace.NewWorkspaceResponse(migrating))
}

func (h *bareMigrationHandler) runMigration(id uint, ws *workspace.Workspace) {
	ctx, cancel := context.WithTimeout(context.Background(), workspace.BareOpTimeout)
	defer cancel()

	if ws.RepoID != nil {
		if err := h.sessionService.DetachWorktreesByRepoID(ctx, *ws.RepoID); err != nil {
			h.workspaceService.FailBareOperation(id, fmt.Errorf("failed to clear worktree records before bare migration: %w", err))
			return
		}
	}

	if err := h.gitService.MigrateToBare(ctx, ws.Path, h.workspaceService.ProgressFn(id)); err != nil {
		h.workspaceService.FailBareOperation(id, err)
		return
	}

	if err := claudesettings.EnsureWorkspaceRoot(ws.Path); err != nil {
		slog.Warn("failed to bootstrap claude settings after bare migration",
			"workspace", ws.Name, "error", err)
	}

	if err := h.workspaceService.FinalizeBareWorkspace(ctx, id); err != nil {
		h.workspaceService.FailBareOperation(id, err)
	}
}
