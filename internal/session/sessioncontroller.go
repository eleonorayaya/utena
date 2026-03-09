package session

import (
	"context"
	"errors"
	"net/http"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/workspace"
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

func (c *SessionController) lookupWorkspace(id string) (*workspace.Workspace, error) {
	if id == "" {
		return nil, nil
	}
	return c.service.workspaceService.GetWorkspace(context.Background(), id)
}

func (c *SessionController) ListSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sessions, err := c.service.ListSessions(ctx)
	if err != nil {
		render.Render(w, r, common.ErrUnknown(err))
		return
	}

	response := NewSessionListResponse(sessions)
	render.Render(w, r, response)
}

func (c *SessionController) GetSessionByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	session, err := c.service.GetSession(ctx, id)
	if err != nil {
		render.Render(w, r, common.ErrNotFound())
		return
	}

	ws, err := c.lookupWorkspace(session.WorkspaceID)
	if err != nil {
		render.Render(w, r, common.ErrUnknown(err))
		return
	}
	render.Render(w, r, NewSessionResponse(session, ws))
}

func (c *SessionController) ListSessionsByWorkspace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := chi.URLParam(r, "workspaceId")

	sessions, err := c.service.ListSessionsByWorkspace(ctx, workspaceID)
	if err != nil {
		render.Render(w, r, common.ErrNotFound())
		return
	}

	response := NewSessionListResponse(sessions)
	render.Render(w, r, response)
}

func (c *SessionController) CreateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	data := &CreateSessionRequest{}
	if err := render.Bind(r, data); err != nil {
		render.Render(w, r, common.ErrInvalidRequest(err))
		return
	}

	session := &Session{
		Name:          data.Name,
		WorkspaceID:   data.WorkspaceID,
		Branch:        data.Branch,
		BaseBranch:    data.BaseBranch,
		BranchCreated: data.BranchCreated,
	}

	if err := c.service.CreateSession(ctx, session, data.CreateWorktree); err != nil {
		var wsNotFound *workspace.WorkspaceNotFoundError
		if errors.Is(err, ErrSessionAlreadyExists) || errors.As(err, &wsNotFound) {
			render.Render(w, r, common.ErrInvalidRequest(err))
			return
		}
		render.Render(w, r, common.ErrUnknown(err))
		return
	}

	ws, err := c.lookupWorkspace(session.WorkspaceID)
	if err != nil {
		render.Render(w, r, common.ErrUnknown(err))
		return
	}
	render.Status(r, http.StatusAccepted)
	render.Render(w, r, NewSessionResponse(session, ws))
}

func (c *SessionController) UpdateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	data := &UpdateSessionRequest{}
	if err := render.Bind(r, data); err != nil {
		render.Render(w, r, common.ErrInvalidRequest(err))
		return
	}

	data.Session.ID = id

	if err := c.service.UpdateSession(ctx, data.Session); err != nil {
		var wsNotFound *workspace.WorkspaceNotFoundError
		if errors.As(err, &wsNotFound) {
			render.Render(w, r, common.ErrInvalidRequest(err))
			return
		}
		render.Render(w, r, common.ErrUnknown(err))
		return
	}

	ws, err := c.lookupWorkspace(data.Session.WorkspaceID)
	if err != nil {
		render.Render(w, r, common.ErrUnknown(err))
		return
	}
	render.Render(w, r, NewSessionResponse(data.Session, ws))
}

func (c *SessionController) DeleteSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	deleteBranch := r.URL.Query().Get("delete_branch") != "false"

	if err := c.service.DeleteSession(ctx, id, deleteBranch); err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			render.Render(w, r, common.ErrNotFound())
			return
		}
		if errors.Is(err, ErrSessionAttached) {
			render.Render(w, r, common.ErrInvalidRequest(err))
			return
		}
		render.Render(w, r, common.ErrUnknown(err))
		return
	}

	render.NoContent(w, r)
}

func (c *SessionController) ActivateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := chi.URLParam(r, "name")

	session, err := c.service.ActivateSession(ctx, name)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			render.Render(w, r, common.ErrNotFound())
		} else if errors.Is(err, ErrCannotActivate) {
			render.Render(w, r, common.ErrInvalidRequest(err))
		} else {
			render.Render(w, r, common.ErrUnknown(err))
		}
		return
	}

	render.Render(w, r, NewSessionResponse(session, nil))
}

func (c *SessionController) RepairSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	session, err := c.service.RepairSession(ctx, id)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			render.Render(w, r, common.ErrNotFound())
		} else if errors.Is(err, ErrSessionNotBroken) {
			render.Render(w, r, common.ErrInvalidRequest(err))
		} else {
			render.Render(w, r, common.ErrUnknown(err))
		}
		return
	}

	render.Render(w, r, NewSessionResponse(session, nil))
}
