package browser

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Router struct {
	controller *Controller
	driver     Driver
}

func NewRouter(controller *Controller, driver Driver) *Router {
	return &Router{controller: controller, driver: driver}
}

func (rt *Router) Routes() chi.Router {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(rejectCrossOrigin)
		r.Post("/open", rt.controller.HandleOpen)
		r.Get("/status", rt.controller.HandleStatus)
	})
	if hd, ok := rt.driver.(HTTPDriver); ok {
		r.Handle("/"+hd.Name(), hd.Handler())
	}
	return r
}

func rejectCrossOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.Header.Values("Origin")) > 0 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
