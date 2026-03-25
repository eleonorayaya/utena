package claude

import (
	"net/http"

	"github.com/eleonorayaya/utena/internal/common"
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
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	if err := c.service.HandleHookEvent(ctx, data); err != nil {
		common.RenderError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{"status": "ok"})
}

func (c *ClaudeController) ListClaudeSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sessions, err := c.service.ListAll(ctx)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	response := NewClaudeSessionListResponse(sessions)
	render.Render(w, r, response)
}
