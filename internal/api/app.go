package api

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tmux"
	"github.com/eleonorayaya/utena/internal/todo"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
)

type App struct {
	db        db.Database
	Workspace *workspace.WorkspaceModule
	Session   *session.SessionModule
	Tmux      *tmux.TmuxModule
	Claude    *claude.ClaudeModule
	Todo      *todo.TodoModule
}

func NewApp(cfg Config) (*App, error) {
	database, err := db.Open(filepath.Join(cfg.ConfigDir, "utena.db"))
	if err != nil {
		return nil, err
	}
	return newApp(database, afero.NewOsFs(), cfg), nil
}

func NewTestApp(cfg Config, tmuxManager session.TmuxManager) *App {
	database, _ := db.OpenInMemory()
	fs := afero.NewMemMapFs()
	bus := eventbus.NewEventBus()

	workspaceModule := workspace.NewWorkspaceModule(database, fs, cfg.ConfigDir)
	tmuxModule := tmux.NewTmuxModule(bus)
	sessionModule := session.NewSessionModule(tmuxManager, workspaceModule, bus, database, cfg.BranchPrefix)

	app := &App{
		db:        database,
		Workspace: workspaceModule,
		Session:   sessionModule,
		Tmux:      tmuxModule,
		Claude:    claude.NewClaudeModule(bus, database),
		Todo:      todo.NewTodoModule(workspaceModule, database),
	}

	database.Migrate(app.collectModels()...)

	return app
}

func newApp(database db.Database, fs afero.Fs, cfg Config) *App {
	bus := eventbus.NewEventBus()

	workspaceModule := workspace.NewWorkspaceModule(database, fs, cfg.ConfigDir)
	tmuxModule := tmux.NewTmuxModule(bus)
	sessionModule := session.NewSessionModule(tmuxModule.Service, workspaceModule, bus, database, cfg.BranchPrefix)

	app := &App{
		db:        database,
		Workspace: workspaceModule,
		Session:   sessionModule,
		Tmux:      tmuxModule,
		Claude:    claude.NewClaudeModule(bus, database),
		Todo:      todo.NewTodoModule(workspaceModule, database),
	}

	database.Migrate(app.collectModels()...)

	return app
}

func (a *App) OnStart(ctx context.Context) error {
	modules := a.modules()
	for _, m := range modules {
		if err := m.module.OnAppStart(ctx); err != nil {
			return err
		}
		slog.Info("Initialized module", "module", m.name)
	}
	return nil
}

func (a *App) OnEnd(ctx context.Context) {
	modules := a.modules()
	for i := len(modules) - 1; i >= 0; i-- {
		m := modules[i]
		if err := m.module.OnAppEnd(ctx); err != nil {
			slog.Error("Error cleaning up module", "module", m.name, "error", err)
		}
	}
	if a.db != nil {
		a.db.Close()
	}
}

func (a *App) Routes() chi.Router {
	r := chi.NewRouter()
	for _, m := range a.modules() {
		r.Mount(m.path, m.module.Routes())
	}
	return r
}

type moduleEntry struct {
	name   string
	path   string
	module common.Module
}

func (a *App) collectModels() []any {
	var models []any
	for _, m := range a.modules() {
		if mp, ok := m.module.(common.ModelProvider); ok {
			models = append(models, mp.Models()...)
		}
	}
	return models
}

func (a *App) modules() []moduleEntry {
	return []moduleEntry{
		{"workspace", "/workspaces", a.Workspace},
		{"tmux", "/tmux", a.Tmux},
		{"session", "/sessions", a.Session},
		{"claude", "/claude", a.Claude},
		{"todo", "/todos", a.Todo},
	}
}
