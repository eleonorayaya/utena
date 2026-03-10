package common

import (
	"context"

	"github.com/go-chi/chi/v5"
)

type Module interface {
	OnAppStart(ctx context.Context) error
	OnAppEnd(ctx context.Context) error
	Routes() chi.Router
}

type ModelProvider interface {
	Models() []any
}
