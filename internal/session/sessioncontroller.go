package session

import (
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

	response := NewSessionResponse(session)
	render.Render(w, r, response)
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

	if err := c.service.CreateSessionAndNotify(ctx, data.Session); err != nil {
		var wsNotFound *workspace.WorkspaceNotFoundError
		if errors.Is(err, ErrSessionAlreadyExists) || errors.As(err, &wsNotFound) {
			render.Render(w, r, common.ErrInvalidRequest(err))
			return
		}
		render.Render(w, r, common.ErrUnknown(err))
		return
	}

	response := NewSessionResponse(data.Session)
	render.Status(r, http.StatusCreated)
	render.Render(w, r, response)
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

	response := NewSessionResponse(data.Session)
	render.Render(w, r, response)
}

func (c *SessionController) DeleteSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	if err := c.service.DeleteSession(ctx, id); err != nil {
		render.Render(w, r, common.ErrNotFound())
		return
	}

	render.NoContent(w, r)
}

func (c *SessionController) ActivateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := chi.URLParam(r, "name")

	_, err := c.service.ActivateSession(ctx, name)
	if err != nil {
		render.Render(w, r, common.ErrNotFound())
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"success": true,
		"message": "Session activated",
	})
}
