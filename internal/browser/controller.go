package browser

import (
	"context"
	"errors"
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
	if errors.Is(err, ErrUnavailable) {
		render.Status(r, http.StatusServiceUnavailable)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		common.RenderError(w, r, err)
		return
	}
	render.JSON(w, r, res)
}

func (c *Controller) HandleStatus(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, map[string]bool{"connected": c.service.Connected()})
}
