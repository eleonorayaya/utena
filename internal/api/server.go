package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httplog/v3"
	"github.com/go-chi/render"
)

func BuildServer(app *App, cfg Config) *http.Server {
	r := chi.NewRouter()

	httplogOpts := &httplog.Options{
		LogExtraAttrs: func(r *http.Request, _ string, _ int) []slog.Attr {
			if s := r.Header.Get("X-Zellij-Session"); s != "" {
				return []slog.Attr{slog.String("zellij_session", s)}
			}
			return nil
		},
	}
	if cfg.PrettyLogs {
		httplogOpts.Schema = httplog.SchemaECS.Concise(true)
	}

	r.Use(httplog.RequestLogger(slog.Default(), httplogOpts))
	r.Use(middleware.Recoverer)
	r.Use(middleware.URLFormat)
	r.Use(render.SetContentType(render.ContentTypeJSON))

	r.Mount("/", app.Routes())

	return &http.Server{
		Addr:    cfg.Addr(),
		Handler: r,
	}
}
