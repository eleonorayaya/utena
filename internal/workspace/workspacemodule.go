package workspace

import (
	"context"
	"log/slog"
	"time"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/jobs"
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

func NewWorkspaceModule(database db.Database, fs afero.Fs, configDir string, gitService *git.GitService) *WorkspaceModule {
	store := NewWorkspaceStore(database, fs, configDir)
	service := NewWorkspaceService(store)
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

func (m *WorkspaceModule) Models() []any {
	return []any{&Workspace{}}
}

func (m *WorkspaceModule) Routes() chi.Router {
	return m.Router.Routes()
}

func (m *WorkspaceModule) RegisterJobs(svc *jobs.JobService) {
	svc.Register(&repoSyncTask{store: m.Store, gitService: m.GitService})
}

type repoSyncTask struct {
	store      *WorkspaceStore
	gitService *git.GitService
}

func (t *repoSyncTask) Name() string            { return "workspace.repos" }
func (t *repoSyncTask) Interval() time.Duration { return 5 * time.Minute }
func (t *repoSyncTask) Run(ctx context.Context) error {
	workspaces := t.store.List()
	for _, ws := range workspaces {
		if !ws.IsGitRepo || ws.RepoID != nil {
			continue
		}
		slog.Info("syncing repo for workspace", "workspace", ws.Name, "path", ws.Path)
		repo, err := t.gitService.FindOrCreateRepo(ctx, ws.Path)
		if err != nil {
			slog.Warn("failed to sync repo for workspace", "workspace", ws.Name, "error", err)
			continue
		}
		ws.RepoID = &repo.ID
		if err := t.store.Update(&ws); err != nil {
			slog.Warn("failed to update workspace repo", "workspace", ws.Name, "error", err)
		}
	}
	return nil
}
