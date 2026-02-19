package claude

import (
	"net/http"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type ClaudeController struct {
	service *ClaudeService
}

func NewClaudeController(service *ClaudeService) *ClaudeController {
	return &ClaudeController{
		service: service,
	}
}

func (c *ClaudeController) HandleHookEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	data := &HookEventRequest{}
	if err := render.Bind(r, data); err != nil {
		render.Render(w, r, common.ErrInvalidRequest(err))
		return
	}

	if err := c.service.HandleHookEvent(ctx, data); err != nil {
		render.Render(w, r, common.ErrUnknown(err))
		return
	}

	render.JSON(w, r, map[string]string{"status": "ok"})
}

func (c *ClaudeController) ListClaudeSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sessions, err := c.service.ListAll(ctx)
	if err != nil {
		render.Render(w, r, common.ErrUnknown(err))
		return
	}

	response := NewClaudeSessionListResponse(sessions)
	render.Render(w, r, response)
}

func (c *ClaudeController) ListClaudeSessionsBySession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sessionID := chi.URLParam(r, "sessionId")

	sessions, err := c.service.ListBySession(ctx, sessionID)
	if err != nil {
		render.Render(w, r, common.ErrUnknown(err))
		return
	}

	response := NewClaudeSessionListResponse(sessions)
	render.Render(w, r, response)
}
