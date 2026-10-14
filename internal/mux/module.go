package mux

import (
	"context"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/go-chi/chi/v5"
)

type Module struct {
	Service    *Service
	Controller *Controller
	Router     *Router
}

func NewModule(bus eventbus.EventBus, database db.Database) *Module {
	runner := newTuiosRunner()
	store := NewStore(database)
	service := NewService(runner, store, bus)
	controller := NewController(service)
	router := NewRouter(controller)
	return &Module{Service: service, Controller: controller, Router: router}
}

func NewModuleWithRunner(runner tmuxRunner, store *Store, bus eventbus.EventBus) *Module {
	service := NewService(runner, store, bus)
	controller := NewController(service)
	router := NewRouter(controller)
	return &Module{Service: service, Controller: controller, Router: router}
}

func (m *Module) OnAppStart(ctx context.Context) error {
	return m.Service.OnAppStart(ctx)
}

func (m *Module) OnAppEnd(ctx context.Context) error {
	return m.Service.OnAppEnd(ctx)
}

func (m *Module) Routes() chi.Router {
	return m.Router.Routes()
}

func (m *Module) Models() []any {
	return []any{&Session{}}
}
