package api

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/eleonorayaya/utena/internal/aerospace"
	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/bundles"
	"github.com/eleonorayaya/utena/internal/chrome"
	"github.com/eleonorayaya/utena/internal/chromescript"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/claudesettings"
	"github.com/eleonorayaya/utena/internal/coder"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/firefox"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/jobs"
	"github.com/eleonorayaya/utena/internal/monitor"
	"github.com/eleonorayaya/utena/internal/mux"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tuios"
	"github.com/eleonorayaya/utena/internal/wm"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
	"gorm.io/gorm"
)

type App struct {
	DB        *db.DatabaseModule
	Git       *git.GitModule
	Workspace *workspace.WorkspaceModule
	Session   *session.SessionModule
	Bundles   *bundles.Module
	Browser   common.Module
	Mux       *mux.Module
	Claude    *claude.ClaudeModule
	Jobs      *jobs.JobsModule
	Monitor   *monitor.MonitorModule
	Coder     *coder.Module
}

func NewApp(cfg Config) (*App, error) {
	gormDB, err := db.OpenSQLite(filepath.Join(cfg.ConfigDir, "utena.db"))
	if err != nil {
		return nil, err
	}
	app := newApp(gormDB, afero.NewOsFs(), cfg)
	if path, err := claudesettings.ClaudeGlobalConfigPath(); err != nil {
		slog.Warn("resolve claude global config path failed; session roots won't be trusted", "error", err)
	} else {
		app.Session.Service.SetClaudeGlobalConfigPath(path)
	}
	return app, nil
}

func newApp(gormDB *gorm.DB, fs afero.Fs, cfg Config) *App {
	return buildApp(gormDB, fs, cfg, nil, nil, nil)
}

func buildApp(gormDB *gorm.DB, fs afero.Fs, cfg Config, muxModule *mux.Module, gitMod *git.GitModule, bus eventbus.EventBus) *App {
	if bus == nil {
		bus = eventbus.NewEventBus()
	}

	dbModule := db.NewDatabaseModule(gormDB)
	database := dbModule.Service

	gitModule := gitMod
	if gitModule == nil {
		gitModule = git.NewGitModule(database, bus)
	}
	workspaceModule := workspace.NewWorkspaceModule(database, fs, cfg.ConfigDir, gitModule.Service)
	if muxModule == nil {
		muxModule = mux.NewModule(tuios.NewDriver(), bus, database)
	}
	sessionModule := session.NewSessionModule(muxModule.Service, workspaceModule, bus, database, cfg.BranchPrefix, cfg.ConfigDir, cfg.SessionsRoot)
	sessionModule.Service.SetBundleCatalog(cfg.Bundles, cfg.McpServers)
	bundlesModule := bundles.NewModule(cfg.Bundles)
	sessions := browserSessions{sessions: sessionModule.Service}
	var browserModule common.Module
	if cfg.Follower {
		gitModule.DisableGitHub()
		sessionModule.DisableGitHub()
		sessionModule.Service.SetSourceReader(workspace.NewLeaderSourceReader(cfg.LeaderURL, nil))
		browserModule = browser.NewFollowerModule(cfg.LeaderURL, nil, sessions)
	} else {
		browserModule = browser.NewModule(newBrowserDriver(cfg.Browser), sessions, newWMDriver(cfg.WM), browser.Layout{BrowserWorkspace: cfg.BrowserWorkspace, ParkWorkspace: cfg.ParkWorkspace}, bus, database)
	}

	coderCfg := cfg.Coder
	if cfg.Follower {
		coderCfg = coder.Config{}
	}
	coderModule := coder.NewModule(coderCfg, nil, sessionModule.Service)
	sessionModule.Service.SetRemoteBackend(coderModule.Service)

	jobsModule := jobs.NewJobsModule()
	gitModule.RegisterJobs(jobsModule.Service)
	sessionModule.RegisterJobs(jobsModule.Service)
	workspaceModule.RegisterJobs(jobsModule.Service)

	app := &App{
		DB:        dbModule,
		Git:       gitModule,
		Workspace: workspaceModule,
		Session:   sessionModule,
		Bundles:   bundlesModule,
		Browser:   browserModule,
		Mux:       muxModule,
		Claude:    claude.NewClaudeModule(database),
		Jobs:      jobsModule,
		Monitor:   monitor.NewMonitorModule(bus, sessionModule.Service),
		Coder:     coderModule,
	}

	database.RegisterModels(app.collectModels()...)
	database.RegisterPreMigrations(app.collectPreMigrations()...)

	return app
}

var browserDrivers = map[string]func() browser.Driver{
	chrome.Browser.Name:  func() browser.Driver { return chrome.NewDriver() },
	firefox.Browser.Name: func() browser.Driver { return firefox.NewDriver() },
	chromescript.Name:    func() browser.Driver { return chromescript.NewDriver() },
}

func newBrowserDriver(name string) browser.Driver {
	return browserDrivers[name]()
}

func newWMDriver(name string) wm.Driver {
	if name == "aerospace" {
		return aerospace.NewDriver()
	}
	return wm.None{}
}

func (a *App) OnStart(ctx context.Context) error {
	if err := a.DB.OnAppStart(ctx); err != nil {
		return err
	}
	slog.Info("Initialized module", "module", "database")

	for _, m := range a.modules() {
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
	if err := a.DB.OnAppEnd(ctx); err != nil {
		slog.Error("Error closing database", "error", err)
	}
}

func (a *App) Routes() chi.Router {
	r := chi.NewRouter()
	for _, m := range a.modules() {
		r.Mount(m.path, m.module.Routes())
	}
	bareMigration := &bareMigrationHandler{
		workspaceService: a.Workspace.Service,
		gitService:       a.Git.Service,
		sessionService:   a.Session.Service,
	}
	r.Post("/workspaces/{id}/migrate-bare", bareMigration.handle)
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

func (a *App) collectPreMigrations() []db.PreMigration {
	var fns []db.PreMigration
	for _, m := range a.modules() {
		if pp, ok := m.module.(common.PreMigrationProvider); ok {
			for _, fn := range pp.PreMigrations() {
				fns = append(fns, func(tx *gorm.DB) error {
					if err := fn(tx); err != nil {
						return fmt.Errorf("%s: %w", m.name, err)
					}
					return nil
				})
			}
		}
	}
	return fns
}

func (a *App) modules() []moduleEntry {
	return []moduleEntry{
		{"git", "/git", a.Git},
		{"mux", "/mux", a.Mux},
		{"workspace", "/workspaces", a.Workspace},
		{"claude", "/claude", a.Claude},
		{"session", "/sessions", a.Session},
		{"coder", "/coder", a.Coder},
		{"bundles", "/bundles", a.Bundles},
		{"browser", "/browser", a.Browser},
		{"jobs", "/jobs", a.Jobs},
		{"monitor", "/monitor", a.Monitor},
	}
}
