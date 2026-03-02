package tmux

import (
	"context"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/go-chi/chi/v5"
)

type TmuxModule struct {
	Service    *TmuxService
	Controller *TmuxController
	Router     *TmuxRouter
}

func NewTmuxModule(sessionModule *session.SessionModule, bus eventbus.EventBus) *TmuxModule {
	service := NewTmuxService(sessionModule.Service, bus)
	controller := NewTmuxController(service)
	router := NewTmuxRouter(controller)
	return &TmuxModule{Service: service, Controller: controller, Router: router}
}

func (m *TmuxModule) OnAppStart(ctx context.Context) error {
	return m.Service.OnAppStart(ctx)
}

func (m *TmuxModule) OnAppEnd(ctx context.Context) error {
	return m.Service.OnAppEnd(ctx)
}

func (m *TmuxModule) Routes() chi.Router {
	return m.Router.Routes()
}
