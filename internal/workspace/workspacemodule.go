package workspace

import (
	"context"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
)

type WorkspaceModule struct {
	Store      *WorkspaceStore
	Service    *WorkspaceService
	Controller *WorkspaceController
	Router     *WorkspaceRouter
	GitService *git.GitService
}

func NewWorkspaceModule(database db.Database, fs afero.Fs, configDir string) *WorkspaceModule {
	store := NewWorkspaceStore(database, fs, configDir)
	service := NewWorkspaceService(store)
	gitService := git.NewGitService()
	controller := NewWorkspaceController(service, gitService)
	router := NewWorkspaceRouter(controller)

	return &WorkspaceModule{
		Store:      store,
		Service:    service,
		Controller: controller,
		Router:     router,
		GitService: gitService,
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
