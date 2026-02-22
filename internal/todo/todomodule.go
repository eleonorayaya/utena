package todo

import (
	"context"

	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
)

type TodoModule struct {
	Store      *TodoStore
	Service    *TodoService
	Controller *TodoController
	Router     *TodoRouter
}

func NewTodoModule(workspaceModule *workspace.WorkspaceModule, fs afero.Fs, configDir string) *TodoModule {
	store := NewTodoStore(fs, configDir)
	service := NewTodoService(store, workspaceModule.Service)
	controller := NewTodoController(service)
	router := NewTodoRouter(controller)

	return &TodoModule{
		Store:      store,
		Service:    service,
		Controller: controller,
		Router:     router,
	}
}

func (m *TodoModule) OnAppStart(ctx context.Context) error {
	if err := m.Store.OnAppStart(ctx); err != nil {
		return err
	}

	if err := m.Service.OnAppStart(ctx); err != nil {
		return err
	}

	return nil
}

func (m *TodoModule) OnAppEnd(ctx context.Context) error {
	if err := m.Service.OnAppEnd(ctx); err != nil {
		return err
	}

	if err := m.Store.OnAppEnd(ctx); err != nil {
		return err
	}

	return nil
}

func (m *TodoModule) Routes() chi.Router {
	return m.Router.Routes()
}
