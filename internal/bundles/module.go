package bundles

import (
	"context"
	"net/http"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/go-chi/chi/v5"
)

type Module struct {
	catalog Catalog
}

func NewModule(catalog Catalog) *Module {
	return &Module{catalog: catalog}
}

func (m *Module) OnAppStart(context.Context) error { return nil }
func (m *Module) OnAppEnd(context.Context) error   { return nil }

func (m *Module) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", m.list)
	return r
}

type ListResponse struct {
	Bundles []Entry `json:"bundles"`
}

func (*ListResponse) Render(http.ResponseWriter, *http.Request) error { return nil }

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	common.RenderResponse(w, r, &ListResponse{Bundles: m.catalog.Entries()})
}
