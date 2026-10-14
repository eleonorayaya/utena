package mux

import (
	"context"
	"fmt"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

type Module struct {
	Service    *Service
	Controller *Controller
	Router     *Router
}

func NewModule(driver Driver, bus eventbus.EventBus, database db.Database) *Module {
	store := NewStore(database)
	service := NewService(driver, store, bus)
	controller := NewController(service)
	router := NewRouter(controller)
	return &Module{Service: service, Controller: controller, Router: router}
}

func NewModuleWithDriver(driver Driver, store *Store, bus eventbus.EventBus) *Module {
	service := NewService(driver, store, bus)
	controller := NewController(service)
	router := NewRouter(controller)
	return &Module{Service: service, Controller: controller, Router: router}
}

func (m *Module) OnAppStart(ctx context.Context) error {
	return m.Service.OnAppStart(ctx)
}

func (m *Module) OnAppEnd(ctx context.Context) error {
	return m.Service.OnAppEnd(ctx)
}

func (m *Module) Routes() chi.Router {
	return m.Router.Routes()
}

func (m *Module) Models() []any {
	return []any{&Session{}}
}

func (m *Module) PreMigrations() []db.PreMigration {
	return []db.PreMigration{renameTmuxSessionsTable}
}

func renameTmuxSessionsTable(tx *gorm.DB) error {
	mg := tx.Migrator()
	if mg.HasTable("tmux_sessions") {
		if mg.HasTable("mux_sessions") {
			return fmt.Errorf("both tmux_sessions and mux_sessions tables exist")
		}
		if err := mg.RenameTable("tmux_sessions", "mux_sessions"); err != nil {
			return err
		}
	}
	if !mg.HasTable("mux_sessions") {
		return nil
	}
	for _, idx := range []string{"idx_tmux_sessions_name", "idx_tmux_sessions_status", "idx_tmux_sessions_deleted_at"} {
		if mg.HasIndex("mux_sessions", idx) {
			if err := mg.DropIndex("mux_sessions", idx); err != nil {
				return err
			}
		}
	}
	return nil
}
