package monitor

import (
	"context"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/go-chi/chi/v5"
)

type MonitorModule struct {
	Service    *MonitorService
	Controller *MonitorController
	Router     *MonitorRouter
}

func NewMonitorModule(bus eventbus.EventBus, snapshots SnapshotProvider) *MonitorModule {
	service := NewMonitorService(bus, snapshots)
	controller := NewMonitorController(service)
	router := NewMonitorRouter(controller)

	return &MonitorModule{
		Service:    service,
		Controller: controller,
		Router:     router,
	}
}

func (m *MonitorModule) OnAppStart(ctx context.Context) error {
	return m.Service.OnAppStart(ctx)
}

func (m *MonitorModule) OnAppEnd(ctx context.Context) error {
	return m.Service.OnAppEnd(ctx)
}

func (m *MonitorModule) Routes() chi.Router {
	return m.Router.Routes()
}
