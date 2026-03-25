package session

import (
	"net/http"
	"strconv"

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

	sessions, err := c.service.ListSessions(ctx)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	response := NewSessionListResponse(sessions)
	render.Render(w, r, response)
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

	render.Render(w, r, NewSessionResponse(session))
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

	response := NewSessionListResponse(sessions)
	render.Render(w, r, response)
}

func (c *SessionController) CreateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	data := &CreateSessionRequest{}
	if err := render.Bind(r, data); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	session := &Session{
		Name:        data.Name,
		WorkspaceID: data.WorkspaceID,
		Branch:      data.Branch,
		BaseBranch:  data.BaseBranch,
		TodoID:      data.TodoID,
	}

	if err := c.service.CreateSession(ctx, session, data.CreateWorktree); err != nil {
		common.RenderError(w, r, err)
		return
	}

	render.Status(r, http.StatusAccepted)
	render.Render(w, r, NewSessionResponse(session))
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

	data.Session.ID = id

	if err := c.service.UpdateSession(ctx, data.Session); err != nil {
		common.RenderError(w, r, err)
		return
	}

	render.Render(w, r, NewSessionResponse(data.Session))
}

func (c *SessionController) DeleteSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUintParam(r, "id")
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	deleteBranch := r.URL.Query().Get("delete_branch") != "false"

	if err := c.service.DeleteSession(ctx, id, deleteBranch); err != nil {
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

	render.Render(w, r, NewSessionResponse(session))
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

	render.Render(w, r, NewSessionResponse(session))
}
