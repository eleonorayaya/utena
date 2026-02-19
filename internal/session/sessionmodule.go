package session

import (
	"context"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
)

type SessionModule struct {
	Store      *SessionStore
	Service    *SessionService
	Controller *SessionController
	Router     *SessionRouter
}

func NewSessionModule(workspaceModule *workspace.WorkspaceModule, bus eventbus.EventBus, fs afero.Fs, configDir string) *SessionModule {
	store := NewSessionStore(fs, configDir)
	service := NewSessionService(store, workspaceModule.Service, workspaceModule.GitService, bus)
	controller := NewSessionController(service)
	router := NewSessionRouter(controller)

	return &SessionModule{
		Store:      store,
		Service:    service,
		Controller: controller,
		Router:     router,
	}
}

func (m *SessionModule) OnAppStart(ctx context.Context) error {
	if err := m.Store.OnAppStart(ctx); err != nil {
		return err
	}

	if err := m.Service.OnAppStart(ctx); err != nil {
		return err
	}

	return nil
}

func (m *SessionModule) OnAppEnd(ctx context.Context) error {
	if err := m.Service.OnAppEnd(ctx); err != nil {
		return err
	}

	if err := m.Store.OnAppEnd(ctx); err != nil {
		return err
	}

	return nil
}

func (m *SessionModule) Routes() chi.Router {
	return m.Router.Routes()
}
