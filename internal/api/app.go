package api

import (
	"context"
	"log/slog"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/todo"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/eleonorayaya/utena/internal/zellij"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
)

type App struct {
	Workspace *workspace.WorkspaceModule
	Session   *session.SessionModule
	Zellij    *zellij.ZellijModule
	Claude    *claude.ClaudeModule
	Todo      *todo.TodoModule
}

func NewApp(cfg Config) *App {
	return newApp(afero.NewOsFs(), cfg)
}

func NewTestApp(cfg Config) *App {
	return newApp(afero.NewMemMapFs(), cfg)
}

func newApp(fs afero.Fs, cfg Config) *App {
	bus := eventbus.NewEventBus()

	workspaceModule := workspace.NewWorkspaceModule(fs, cfg.ConfigDir)
	sessionModule := session.NewSessionModule(workspaceModule, bus, fs, cfg.ConfigDir, cfg.BranchPrefix)

	return &App{
		Workspace: workspaceModule,
		Session:   sessionModule,
		Zellij:    zellij.NewZellijModule(sessionModule, bus),
		Claude:    claude.NewClaudeModule(bus, fs, cfg.ConfigDir),
		Todo:      todo.NewTodoModule(workspaceModule, fs, cfg.ConfigDir),
	}
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

func (a *App) modules() []moduleEntry {
	return []moduleEntry{
		{"workspace", "/workspaces", a.Workspace},
		{"session", "/sessions", a.Session},
		{"zellij", "/zellij", a.Zellij},
		{"claude", "/claude", a.Claude},
		{"todo", "/todos", a.Todo},
	}
}
