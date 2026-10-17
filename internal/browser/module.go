package browser

import (
	"context"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/wm"
	"github.com/go-chi/chi/v5"
)

type Module struct {
	Service    *Service
	Controller *Controller
	Router     *Router
}

func NewModule(driver Driver, sessions Sessions, wmDriver wm.Driver, layout Layout, bus eventbus.EventBus, database db.Database) *Module {
	service := NewService(driver, NewStore(database, driver.Name()), sessions, wmDriver, layout)
	bus.Subscribe(eventbus.SessionAttached, func(ctx context.Context, e eventbus.Event) error {
		if ev, ok := e.Data.(eventbus.SessionIDEvent); ok {
			service.SetAttached(ev.SessionID)
		}
		return nil
	})
	bus.Subscribe(eventbus.SessionArchived, func(ctx context.Context, e eventbus.Event) error {
		if ev, ok := e.Data.(eventbus.SessionIDEvent); ok {
			service.QueueArchive(ev.SessionID)
		}
		return nil
	})
	bus.Subscribe(eventbus.SessionDeleted, func(ctx context.Context, e eventbus.Event) error {
		if ev, ok := e.Data.(eventbus.SessionIDEvent); ok {
			service.QueueDelete(ev.SessionID)
		}
		return nil
	})
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
