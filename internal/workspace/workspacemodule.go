package workspace

import (
	"context"

	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
)

type WorkspaceModule struct {
	Store      *WorkspaceStore
	Service    *WorkspaceService
	Controller *WorkspaceController
	Router     *WorkspaceRouter
}

func NewWorkspaceModule(fs afero.Fs, configDir string) *WorkspaceModule {
	store := NewWorkspaceStore(fs, configDir)
	service := NewWorkspaceService(store)
	controller := NewWorkspaceController(service)
	router := NewWorkspaceRouter(controller)

	return &WorkspaceModule{
		Store:      store,
		Service:    service,
		Controller: controller,
		Router:     router,
	}
}

func (m *WorkspaceModule) OnAppStart(ctx context.Context) error {

	if err := m.Store.OnAppStart(ctx); err != nil {
		return err
	}

	if err := m.Service.OnAppStart(ctx); err != nil {
		return err
	}

	return nil
}

func (m *WorkspaceModule) OnAppEnd(ctx context.Context) error {

	if err := m.Service.OnAppEnd(ctx); err != nil {
		return err
	}

	if err := m.Store.OnAppEnd(ctx); err != nil {
		return err
	}

	return nil
}

func (m *WorkspaceModule) Routes() chi.Router {
	return m.Router.Routes()
}
