package claude

import (
	"context"

	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
)

type ClaudeModule struct {
	Store      *ClaudeStore
	Service    *ClaudeService
	Controller *ClaudeController
	Router     *ClaudeRouter
}

func NewClaudeModule(fs afero.Fs, configDir string) *ClaudeModule {
	store := NewClaudeStore(fs, configDir)
	service := NewClaudeService(store)
	controller := NewClaudeController(service)
	router := NewClaudeRouter(controller)

	return &ClaudeModule{
		Store:      store,
		Service:    service,
		Controller: controller,
		Router:     router,
	}
}

func (m *ClaudeModule) OnAppStart(ctx context.Context) error {
	if err := m.Store.OnAppStart(ctx); err != nil {
		return err
	}

	if err := m.Service.OnAppStart(ctx); err != nil {
		return err
	}

	return nil
}

func (m *ClaudeModule) OnAppEnd(ctx context.Context) error {
	if err := m.Service.OnAppEnd(ctx); err != nil {
		return err
	}

	if err := m.Store.OnAppEnd(ctx); err != nil {
		return err
	}

	return nil
}

func (m *ClaudeModule) Routes() chi.Router {
	return m.Router.Routes()
}
