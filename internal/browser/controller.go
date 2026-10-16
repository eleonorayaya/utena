package browser

import (
	"context"
	"net/http"
	"time"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/go-chi/render"
)

type Controller struct {
	service *Service
}

func NewController(service *Service) *Controller {
	return &Controller{service: service}
}

func (c *Controller) HandleOpen(w http.ResponseWriter, r *http.Request) {
	req := &OpenRequest{}
	if err := render.Bind(r, req); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
	defer cancel()

	res, err := c.service.Open(ctx, *req)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}
	render.JSON(w, r, res)
}

func (c *Controller) HandleStatus(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, map[string]bool{"connected": c.service.Connected(), "outdated": c.service.Outdated()})
}

func (c *Controller) HandleSessions(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, c.service.BrowserSessions(r.Context()))
}

func (c *Controller) HandleFocus(w http.ResponseWriter, r *http.Request) {
	req := &FocusRequest{}
	if err := render.Bind(r, req); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
	defer cancel()

	res, err := c.service.FocusTab(ctx, *req)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}
	render.JSON(w, r, res)
}
