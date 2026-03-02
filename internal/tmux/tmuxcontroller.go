package tmux

import (
	"net/http"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type TmuxController struct {
	service *TmuxService
}

func NewTmuxController(service *TmuxService) *TmuxController {
	return &TmuxController{
		service: service,
	}
}

func (c *TmuxController) HandleHook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	event := chi.URLParam(r, "event")

	req := &HookEvent{}
	if err := render.Bind(r, req); err != nil {
		render.Render(w, r, common.ErrInvalidRequest(err))
		return
	}

	var err error
	switch event {
	case "session-created":
		err = c.service.HandleSessionCreated(ctx, req.SessionName)
	case "session-closed":
		err = c.service.HandleSessionClosed(ctx, req.SessionName)
	case "client-session-changed":
		err = c.service.HandleClientSessionChanged(ctx, req.SessionName)
	case "client-attached":
		err = c.service.HandleClientAttached(ctx, req.SessionName)
	case "client-detached":
		err = c.service.HandleClientDetached(ctx, req.SessionName)
	default:
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "unknown event: " + event})
		return
	}

	if err != nil {
		render.Render(w, r, common.ErrUnknown(err))
		return
	}

	render.JSON(w, r, map[string]string{"status": "ok"})
}
