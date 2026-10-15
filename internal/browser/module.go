package browser

import (
	"context"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/go-chi/chi/v5"
)

type Module struct {
	Service    *Service
	Controller *Controller
	Router     *Router
}

func NewModule(driver Driver, sessions Sessions, database db.Database) *Module {
	service := NewService(driver, NewStore(database), sessions)
	controller := NewController(service)
	return &Module{Service: service, Controller: controller, Router: NewRouter(controller, driver)}
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
	return []any{&BrowserWindow{}, &BrowserState{}}
}
