package mux

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type Controller struct {
	service *Service
}

func NewController(service *Service) *Controller {
	return &Controller{
		service: service,
	}
}

func (c *Controller) HandleHook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	event := chi.URLParam(r, "event")

	req := &HookEvent{}
	if err := render.Bind(r, req); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	var err error
	switch event {
	case "client-session-changed":
		err = c.service.HandleClientSessionChanged(ctx, req.SessionName)
	case "client-attached":
		err = c.service.HandleClientAttached(ctx, req.SessionName)
	case "client-detached":
		err = c.service.HandleClientDetached(ctx, req.SessionName)
	default:
		common.RenderError(w, r, common.NewInvalidRequest("unknown event: "+event))
		return
	}

	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{"status": "ok"})
}

func (c *Controller) HandleGetWindows(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sessionName := chi.URLParam(r, "sessionName")

	windows := c.service.GetWindows(ctx, sessionName)
	render.JSON(w, r, windows)
}

func (c *Controller) HandleGetSessionEnv(w http.ResponseWriter, r *http.Request) {
	value, ok := c.service.SessionEnv(chi.URLParam(r, "sessionName"), chi.URLParam(r, "key"))
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	render.PlainText(w, r, value)
}

func (c *Controller) HandleNextActivation(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer cancel()

	name, err := c.service.WaitForActivation(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		return
	}
	render.JSON(w, r, HookEvent{SessionName: name})
}
