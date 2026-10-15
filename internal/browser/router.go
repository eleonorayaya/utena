package browser

import "github.com/go-chi/chi/v5"

type Router struct {
	controller *Controller
	driver     Driver
}

func NewRouter(controller *Controller, driver Driver) *Router {
	return &Router{controller: controller, driver: driver}
}

func (rt *Router) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/open", rt.controller.HandleOpen)
	r.Get("/status", rt.controller.HandleStatus)
	if hd, ok := rt.driver.(HTTPDriver); ok {
		r.Handle("/"+hd.Name(), hd.Handler())
	}
	return r
}
