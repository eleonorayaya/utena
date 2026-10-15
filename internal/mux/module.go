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
	service := NewService(driver, NewStore(database), bus)
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
			var leftover int64
			if err := tx.Table("tmux_sessions").Unscoped().Count(&leftover).Error; err != nil {
				return err
			}
			if leftover > 0 {
				return fmt.Errorf("both tmux_sessions and mux_sessions tables exist")
			}
			if err := mg.DropTable("tmux_sessions"); err != nil {
				return err
			}
		} else if err := mg.RenameTable("tmux_sessions", "mux_sessions"); err != nil {
			return err
		}
	}
	if !mg.HasTable("mux_sessions") {
		return nil
	}
	return db.DropIndexesIfExist(mg, "mux_sessions", "idx_tmux_sessions_name", "idx_tmux_sessions_status", "idx_tmux_sessions_deleted_at")
}
