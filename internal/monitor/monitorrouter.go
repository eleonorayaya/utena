package monitor

import (
	"github.com/go-chi/chi/v5"
)

type MonitorRouter struct {
	controller *MonitorController
}

func NewMonitorRouter(controller *MonitorController) *MonitorRouter {
	return &MonitorRouter{controller: controller}
}

func (mr *MonitorRouter) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/ws", mr.controller.Watch)

	return r
}
