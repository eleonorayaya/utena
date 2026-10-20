package coder

import (
	"context"
	"net/http"
	"strconv"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/go-chi/chi/v5"
)

type Module struct {
	Service *Service
}

func NewModule(cfg Config, runner Runner, sessions Sessions) *Module {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Module{Service: NewService(cfg, runner, NewConnManager(cfg, runner), sessions)}
}

func (m *Module) OnAppStart(ctx context.Context) error {
	m.Service.Start(ctx)
	return nil
}

func (m *Module) OnAppEnd(context.Context) error {
	m.Service.Stop()
	return nil
}

func (m *Module) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/status", m.status)
	r.Put("/sessions/{id}/refresh", m.refresh)
	return r
}

type StatusResponse struct {
	Configured  bool     `json:"configured"`
	Connections []string `json:"connections"`
}

func (*StatusResponse) Render(http.ResponseWriter, *http.Request) error { return nil }

func (m *Module) status(w http.ResponseWriter, r *http.Request) {
	common.RenderResponse(w, r, &StatusResponse{Configured: m.Service.Enabled(), Connections: m.Service.Connections()})
}

func (m *Module) refresh(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}
	if err := m.Service.Refresh(r.Context(), uint(id)); err != nil {
		common.RenderError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
