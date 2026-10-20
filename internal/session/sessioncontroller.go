package session

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type SessionController struct {
	service *SessionService
}

func NewSessionController(service *SessionService) *SessionController {
	return &SessionController{
		service: service,
	}
}

func parseUintParam(r *http.Request, name string) (uint, error) {
	raw := chi.URLParam(r, name)
	val, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(val), nil
}

func (c *SessionController) ListSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sessions, err := c.service.ListSessions(ctx, parseStatusFilter(r)...)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	common.RenderResponse(w, r, NewSessionListResponse(sessions))
}

func parseStatusFilter(r *http.Request) []SessionStatus {
	var statuses []SessionStatus
	for _, value := range r.URL.Query()["status"] {
		for raw := range strings.SplitSeq(value, ",") {
			if status := strings.TrimSpace(raw); status != "" {
				statuses = append(statuses, SessionStatus(status))
			}
		}
	}
	return statuses
}

func (c *SessionController) ListResources(w http.ResponseWriter, r *http.Request) {
	resp, err := c.service.ResourceUsage(r.Context())
	if err != nil {
		common.RenderError(w, r, err)
		return
	}
	common.RenderResponse(w, r, resp)
}

func (c *SessionController) GetSessionByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUintParam(r, "id")
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	session, err := c.service.GetSession(ctx, id)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	common.RenderResponse(w, r, NewSessionResponse(session))
}

func (c *SessionController) ListSessionsByWorkspace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw := chi.URLParam(r, "workspaceId")
	workspaceID, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	sessions, err := c.service.ListSessionsByWorkspace(ctx, uint(workspaceID))
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	common.RenderResponse(w, r, NewSessionListResponse(sessions))
}

func (c *SessionController) CreateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	data := &CreateSessionRequest{}
	if err := render.Bind(r, data); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	session, err := c.service.CreateSession(ctx, CreateSessionInput{
		Name:            data.Name,
		Workspaces:      data.Workspaces,
		Bundles:         data.Bundles,
		LeaderSessionID: data.LeaderSessionID,
	})
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	render.Status(r, http.StatusAccepted)
	common.RenderResponse(w, r, NewSessionResponse(session))
}

func (c *SessionController) SetBundles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUintParam(r, "id")
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	data := &SetBundlesRequest{}
	if err := render.Bind(r, data); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	session, err := c.service.SetBundles(ctx, id, data.Bundles)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	common.RenderResponse(w, r, NewSessionResponse(session))
}

func (c *SessionController) DefaultBundles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var ids []uint
	for _, raw := range r.URL.Query()["workspace_id"] {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			common.RenderError(w, r, common.NewInvalidRequest(fmt.Sprintf("invalid workspace_id %q", raw)))
			return
		}
		ids = append(ids, uint(id))
	}

	names, err := c.service.DefaultBundlesFor(ctx, ids)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}
	if names == nil {
		names = []string{}
	}
	common.RenderResponse(w, r, &DefaultBundlesResponse{Bundles: names})
}

func (c *SessionController) UpdateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUintParam(r, "id")
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	data := &UpdateSessionRequest{}
	if err := render.Bind(r, data); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	data.ID = id

	if err := c.service.UpdateSession(ctx, data.Session); err != nil {
		common.RenderError(w, r, err)
		return
	}

	common.RenderResponse(w, r, NewSessionResponse(data.Session))
}

func (c *SessionController) DeleteSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUintParam(r, "id")
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	deleteBranch := r.URL.Query().Get("delete_branch") != "false"
	force := r.URL.Query().Get("force") == "true"

	if err := c.service.DeleteSession(ctx, id, deleteBranch, force); err != nil {
		common.RenderError(w, r, err)
		return
	}

	render.NoContent(w, r)
}

func (c *SessionController) ActivateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUintParam(r, "id")
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	session, err := c.service.ActivateSession(ctx, id)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	common.RenderResponse(w, r, NewSessionResponse(session))
}

func (c *SessionController) RepairSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUintParam(r, "id")
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	session, err := c.service.RepairSession(ctx, id)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	common.RenderResponse(w, r, NewSessionResponse(session))
}

func (c *SessionController) ArchiveSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUintParam(r, "id")
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	session, err := c.service.ArchiveSession(ctx, id)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	common.RenderResponse(w, r, NewSessionResponse(session))
}

func (c *SessionController) AddWorkspace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUintParam(r, "id")
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	data := &AddWorkspaceRequest{}
	if err := render.Bind(r, data); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	session, err := c.service.AddWorkspace(ctx, id, data.WorkspaceBranchSpec)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	render.Status(r, http.StatusAccepted)
	common.RenderResponse(w, r, NewSessionResponse(session))
}

func (c *SessionController) BranchHook(w http.ResponseWriter, r *http.Request) {
	data := &BranchHookRequest{}
	if err := render.Bind(r, data); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	if err := c.service.RecordWorktreeBranch(context.WithoutCancel(r.Context()), data.Path, data.Branch); err != nil {
		common.RenderError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (c *SessionController) DismissSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUintParam(r, "id")
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	if err := c.service.DismissSession(ctx, id); err != nil {
		common.RenderError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{"status": "ok"})
}

func (c *SessionController) AddBlocker(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUintParam(r, "id")
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	data := &AddBlockerRequest{}
	if err := render.Bind(r, data); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	session, err := c.service.AddBlocker(ctx, id, AddBlockerInput{Kind: data.Kind, Note: data.Note, URL: data.URL})
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	render.Status(r, http.StatusCreated)
	common.RenderResponse(w, r, NewSessionResponse(session))
}

func (c *SessionController) ResolveBlocker(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, blockerID, err := parseBlockerParams(r)
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	session, err := c.service.ResolveBlocker(ctx, id, blockerID)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	common.RenderResponse(w, r, NewSessionResponse(session))
}

func (c *SessionController) DeleteBlocker(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, blockerID, err := parseBlockerParams(r)
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	if err := c.service.DeleteBlocker(ctx, id, blockerID); err != nil {
		common.RenderError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func parseBlockerParams(r *http.Request) (uint, uint, error) {
	id, err := parseUintParam(r, "id")
	if err != nil {
		return 0, 0, err
	}
	blockerID, err := parseUintParam(r, "blockerId")
	if err != nil {
		return 0, 0, err
	}
	return id, blockerID, nil
}
